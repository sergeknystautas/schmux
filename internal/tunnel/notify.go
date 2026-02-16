package tunnel

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

type NotifyConfig struct {
	NtfyURL string
	Command string
}

func (nc *NotifyConfig) Send(tunnelURL string, message string) error {
	var errs []string

	if nc.NtfyURL != "" {
		if err := sendNtfyNotification(nc.NtfyURL, message, tunnelURL); err != nil {
			errs = append(errs, fmt.Sprintf("ntfy: %v", err))
		} else {
			// Log success with topic but not the auth URL (contains token)
			topic := nc.NtfyURL
			if idx := strings.LastIndex(topic, "/"); idx >= 0 {
				topic = topic[idx+1:]
			}
			fmt.Printf("[remote-access] ntfy notification sent to topic %q\n", topic)
		}
	}

	if nc.Command != "" {
		if err := runNotifyCommand(nc.Command, tunnelURL); err != nil {
			errs = append(errs, fmt.Sprintf("command: %v", err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("notification errors: %s", strings.Join(errs, "; "))
	}
	return nil
}

func sendNtfyNotification(ntfyURL string, title string, body string) error {
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("POST", ntfyURL, strings.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Title", title)

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send notification: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("ntfy returned HTTP %d", resp.StatusCode)
	}
	return nil
}

func runNotifyCommand(command string, tunnelURL string) error {
	cmd := exec.Command("sh", "-c", command)
	cmd.Env = append(os.Environ(), "SCHMUX_REMOTE_URL="+tunnelURL)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
