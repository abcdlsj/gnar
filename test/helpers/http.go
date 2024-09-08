package helpers

import (
	"fmt"
	"net/http"
	"time"
)

func CheckHTTPResponse(url string, expectedStatus int) error {
	client := &http.Client{
		Timeout: 5 * time.Second,
	}

	resp, err := client.Head(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != expectedStatus {
		return fmt.Errorf("unexpected status code: got %v, want %v", resp.StatusCode, expectedStatus)
	}

	return nil
}
