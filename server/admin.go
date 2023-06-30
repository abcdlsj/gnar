package server

import (
	"embed"
	"html/template"
	"io/fs"
	"net/http"
	"strconv"

	"github.com/abcdlsj/gpipe/logger"
	"github.com/abcdlsj/gpipe/proxy"
)

var (
	//go:embed assets
	assetsFs embed.FS
)

func (s *Server) StartAdmin() {
	if s.cfg.AdminPort == 0 {
		return
	}

	tmpl := template.Must(template.New("").ParseFS(assetsFs, "assets/*.html"))

	fe, _ := fs.Sub(assetsFs, "assets/static")
	http.Handle("/static/", http.StripPrefix("/static", http.FileServer(http.FS(fe))))

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		upbw, dnbw, total := proxy.CalculateBandwidth(s.traffics)
		tmpl.ExecuteTemplate(w, "index.html", map[string]interface{}{
			"forwards": s.forwards,
			"traffic": map[string]interface{}{
				"upbw":  upbw,
				"dnbw":  dnbw,
				"total": total,
			},
		})
	})

	logger.InfoF("admin server started on port %d", s.cfg.AdminPort)
	if err := http.ListenAndServe(":"+strconv.Itoa(s.cfg.AdminPort), nil); err != nil {
		logger.FatalF("admin server error: %v", err)
	}
}
