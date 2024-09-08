package terminal

import (
	"fmt"
	"strings"

	"github.com/abcdlsj/cr"
)

func CreateClickableLink(url, displayText string) string {
	return fmt.Sprintf("\033]8;;%s\033\\%s\033]8;;\033\\", url, cr.PWhiteUnderline(displayText))
}

func CreateProxyLink(domain string) string {
	url := domain
	if !strings.HasPrefix(url, "https://") && !strings.HasPrefix(url, "http://") {
		url = "https://" + url
	}
	return CreateClickableLink(url, domain)
}
