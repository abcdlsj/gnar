package server

import (
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"net/http"
	"strconv"

	"github.com/abcdlsj/pipe/logger"
)

var (
	//go:embed assets
	assetsFs embed.FS
)

func (s *Server) startAdmin() {
	tmpl := template.Must(template.New("").ParseFS(assetsFs, "assets/*.html"))

	fe, _ := fs.Sub(assetsFs, "assets/static")
	http.Handle("/static/", http.StripPrefix("/static", http.FileServer(http.FS(fe))))

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if err := tmpl.ExecuteTemplate(w, "index.html", map[string]interface{}{
			"proxys": s.proxys,
		}); err != nil {
			logger.Errorf("execute index.html error: %v", err)
		}
	})

	http.HandleFunc("/admin/tunnel/close", func(w http.ResponseWriter, r *http.Request) {
		type Req struct {
			To int `json:"to"`
		}

		var msg = "Close tunnel success"
		buf, err := io.ReadAll(r.Body)
		if err != nil {
			msg = fmt.Sprintf("Close tunnel failed, err: %s", err)
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(msg))
			return
		}

		var req Req
		err = json.Unmarshal(buf, &req)
		if err != nil {
			msg = fmt.Sprintf("Close tunnel failed, err: %s", err)
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(msg))
			return
		}

		logger.Infof("Receive close admin call, close proxy, port %d", req.To)
		s.removeProxy(req.To)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(msg))
	})

	logger.Infof("Admin server start %d", s.cfg.AdminPort)
	if err := http.ListenAndServe(":"+strconv.Itoa(s.cfg.AdminPort), nil); err != nil {
		logger.Fatalf("Admin server error: %v", err)
	}
}
