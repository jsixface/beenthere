package api

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/sixface/beenthere/internal/httpx"
	"github.com/sixface/beenthere/internal/importers"
	"github.com/sixface/beenthere/internal/store"
)

func (s *Server) importExportRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/imports", s.auth(false, s.importsIndex))
	mux.HandleFunc("GET /api/v1/imports/{id}", s.auth(false, s.importsShow))
	mux.HandleFunc("POST /api/v1/imports", s.auth(true, s.importsCreate))
	mux.HandleFunc("DELETE /api/v1/imports/{id}", s.auth(true, s.importsDestroy))
	mux.HandleFunc("GET /api/v1/export", s.auth(false, s.export))
}

func (s *Server) importsIndex(w http.ResponseWriter, r *http.Request, u *store.User) {
	page := max(httpx.QueryInt(r, "page", 1), 1)
	per := min(max(httpx.QueryInt(r, "per_page", 25), 1), 100)
	list, total, err := s.S.ListImports(r.Context(), u.ID, per, (page-1)*per)
	if err != nil {
		s.fail(w, err)
		return
	}
	w.Header().Set("X-Current-Page", strconv.Itoa(page))
	w.Header().Set("X-Total-Pages", strconv.Itoa((total+per-1)/per))
	httpx.JSON(w, http.StatusOK, list)
}

func (s *Server) importsShow(w http.ResponseWriter, r *http.Request, u *store.User) {
	id, _ := httpx.PathID(r, "id")
	i, err := s.S.GetImport(r.Context(), u.ID, id)
	if err != nil {
		s.fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, i)
}

func (s *Server) importsDestroy(w http.ResponseWriter, r *http.Request, u *store.User) {
	id, _ := httpx.PathID(r, "id")
	if err := s.S.DeleteImport(r.Context(), u.ID, id); err != nil {
		s.fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]string{"message": "Import was successfully deleted"})
}

// importsCreate accepts multipart/form-data with one or more "files" parts
// (optionally "source"). Files are spooled to disk and parsed by the worker
// pool, so request handling stays cheap and memory stays flat.
func (s *Server) importsCreate(w http.ResponseWriter, r *http.Request, u *store.User) {
	max := s.MaxUploadBytes
	if max <= 0 {
		max = 1 << 30
	}
	r.Body = http.MaxBytesReader(w, r.Body, max)
	mr, err := r.MultipartReader()
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "expected multipart/form-data upload")
		return
	}
	forced := ""
	var created []int64
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			httpx.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		if part.FormName() == "source" || part.FormName() == "import[source]" {
			b, _ := io.ReadAll(io.LimitReader(part, 64))
			forced = strings.TrimSpace(string(b))
			continue
		}
		name := filepath.Base(part.FileName())
		if name == "" || name == "." {
			continue
		}
		id, err := s.acceptUpload(r.Context(), u, name, forced, part)
		if err != nil {
			httpx.Error(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
		created = append(created, id)
	}
	if len(created) == 0 {
		httpx.Error(w, http.StatusUnprocessableEntity, "No files provided")
		return
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{"message": "Import queued", "import_ids": created})
}

func (s *Server) acceptUpload(ctx context.Context, u *store.User, name, forced string, body io.Reader) (int64, error) {
	tmp, err := os.CreateTemp("", "beenthere-import-*")
	if err != nil {
		return 0, err
	}
	br := bufio.NewReaderSize(body, 64<<10)
	head, _ := br.Peek(8192)
	src, ok := importers.SourceNames[forced]
	if !ok {
		src, ok = importers.Detect(name, head)
	}
	if !ok {
		tmp.Close()
		os.Remove(tmp.Name())
		return 0, fmt.Errorf("could not determine the format of %q", name)
	}
	if _, err := io.Copy(tmp, br); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return 0, err
	}
	tmp.Close()
	unique := name
	for i := 2; ; i++ {
		taken, err := s.S.ImportNameTaken(ctx, u.ID, unique)
		if err != nil {
			os.Remove(tmp.Name())
			return 0, err
		}
		if !taken {
			break
		}
		unique = fmt.Sprintf("%s (%d)", name, i)
	}
	id, err := s.S.CreateImport(ctx, u.ID, unique, src)
	if err != nil {
		os.Remove(tmp.Name())
		return 0, err
	}
	path := tmp.Name()
	s.Jobs.Enqueue(func(ctx context.Context) {
		defer os.Remove(path)
		s.runImport(ctx, u.ID, id, src, path)
	})
	return id, nil
}

const importBatch = 1000

func (s *Server) runImport(ctx context.Context, userID, importID int64, src int, path string) {
	_ = s.S.MarkImportProcessing(ctx, importID)
	f, err := os.Open(path)
	if err != nil {
		_ = s.S.FinishImport(ctx, importID, err.Error())
		return
	}
	defer f.Close()

	batch := make([]store.PointIn, 0, importBatch)
	var raw, processed, doubles int
	lo, hi := int64(1<<62), int64(-1<<62)
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		res, err := s.S.UpsertPoints(ctx, userID, batch)
		if err != nil {
			return err
		}
		processed += res.Inserted
		doubles += len(batch) - res.Inserted
		batch = batch[:0]
		_ = s.S.UpdateImportProgress(ctx, importID, processed, raw, doubles)
		return nil
	}
	err = importers.Parse(src, f, func(p store.PointIn) error {
		raw++
		id := importID
		p.ImportID = &id
		lo, hi = min(lo, p.Timestamp), max(hi, p.Timestamp)
		batch = append(batch, p)
		if len(batch) >= importBatch {
			return flush()
		}
		return nil
	})
	if err == nil {
		err = flush()
	}
	if err != nil {
		s.Log.Error("import failed", "import", importID, "err", err)
		_ = s.S.FinishImport(ctx, importID, err.Error())
		return
	}
	_ = s.S.FinishImport(ctx, importID, "")
	if raw > 0 {
		s.Jobs.Recompute(ctx, userID, lo, hi)
	}
}

// export streams the user's points as GPX or GeoJSON.
func (s *Server) export(w http.ResponseWriter, r *http.Request, u *store.User) {
	q := r.URL.Query()
	format := strings.ToLower(q.Get("format"))
	if format == "" {
		format = "geojson"
	}
	from, to := int64(-1<<31), int64(1<<31-1)
	if v := q.Get("start_at"); v != "" {
		from = safeTimestamp(v)
	}
	if v := q.Get("end_at"); v != "" {
		to = safeTimestamp(v)
	}
	stamp := time.Now().UTC().Format("20060102T150405")
	bw := bufio.NewWriterSize(w, 64<<10)
	defer bw.Flush()
	switch format {
	case "gpx":
		w.Header().Set("Content-Type", "application/gpx+xml")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="export_%s.gpx"`, stamp))
		fmt.Fprint(bw, `<?xml version="1.0" encoding="UTF-8"?>`+"\n"+`<gpx xmlns="http://www.topografix.com/GPX/1/1" version="1.1" creator="Dawarich">`+"\n  <trk>\n    <name>dawarich_export</name>\n    <trkseg>\n")
		err := s.S.ExportRows(r.Context(), u.ID, from, to, func(lon, lat float64, ts int64, alt *float64, vel *string, _ map[string]any) error {
			fmt.Fprintf(bw, "      <trkpt lat=\"%v\" lon=\"%v\">\n", lat, lon)
			if alt != nil {
				fmt.Fprintf(bw, "        <ele>%v</ele>\n", *alt)
			}
			fmt.Fprintf(bw, "        <time>%s</time>\n", time.Unix(ts, 0).UTC().Format(time.RFC3339))
			if vel != nil {
				if f, err := strconv.ParseFloat(*vel, 64); err == nil && f > 0 {
					fmt.Fprintf(bw, "        <speed>%v</speed>\n", f)
				}
			}
			_, err := bw.WriteString("      </trkpt>\n")
			return err
		})
		if err != nil {
			s.Log.Error("export", "err", err)
		}
		fmt.Fprint(bw, "    </trkseg>\n  </trk>\n</gpx>\n")
	case "geojson", "json":
		w.Header().Set("Content-Type", "application/geo+json")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="export_%s.geojson"`, stamp))
		bw.WriteString(`{"type":"FeatureCollection","features":[`)
		first := true
		enc := json.NewEncoder(bw)
		err := s.S.ExportRows(r.Context(), u.ID, from, to, func(lon, lat float64, ts int64, alt *float64, vel *string, extra map[string]any) error {
			if !first {
				bw.WriteByte(',')
			}
			first = false
			props := map[string]any{"timestamp": ts, "altitude": alt, "velocity": vel}
			for k, v := range extra {
				props[k] = v
			}
			return enc.Encode(map[string]any{"type": "Feature",
				"geometry": map[string]any{"type": "Point", "coordinates": []float64{lon, lat}}, "properties": props})
		})
		if err != nil {
			s.Log.Error("export", "err", err)
		}
		bw.WriteString(`]}`)
	default:
		httpx.Error(w, http.StatusBadRequest, "format must be gpx or geojson")
	}
}
