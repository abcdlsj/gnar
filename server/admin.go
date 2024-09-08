package server

import (
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"strconv"

	"github.com/abcdlsj/gnar/logger"
)

var (
	//go:embed tmpl
	tmplFs embed.FS
)

func (s *Server) startAdmin() {
	tmpl := template.Must(template.New("").ParseFS(tmplFs, "tmpl/*.html"))

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if err := tmpl.ExecuteTemplate(w, "index.html", map[string]any{
			"proxys": s.resources.proxys, // 更改 s.rm 为 s.resources
		}); err != nil {
			logger.Errorf("execute index.html error: %v", err)
		}
	})

	http.HandleFunc("/admin/tunnel/close", func(w http.ResponseWriter, r *http.Request) {
		type Req struct {
			Port int `json:"port"`
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

		logger.Infof("Receive close admin call, close proxy, port %d", req.Port)
		s.resources.removeProxy(req.Port)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(msg))
	})

	logger.Infof("Admin server start %d", s.cfg.AdminPort)
	if err := http.ListenAndServe(":"+strconv.Itoa(s.cfg.AdminPort), nil); err != nil {
		logger.Fatalf("Admin server error: %v", err)
	}
}
