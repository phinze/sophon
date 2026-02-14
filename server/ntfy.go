package server

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/phinze/sophon/store"
)

// sendNotification sends an ntfy push notification for a session event.
func (s *Server) sendNotification(sess *store.Session, notificationType, message string) {
	if s.cfg.NtfyURL == "" {
		return
	}

	var title, priority, tags string

	switch notificationType {
	case "permission_prompt":
		title = fmt.Sprintf("[%s] Needs approval", sess.Project)
		priority = "high"
		tags = "lock"
	default:
		title = fmt.Sprintf("[%s] Waiting for input", sess.Project)
		priority = "default"
		tags = "hourglass_flowing_sand"
	}

	clickURL := fmt.Sprintf("%s/sophon/respond/%s", strings.TrimRight(s.cfg.BaseURL, "/"), sess.ID)

	req, err := http.NewRequest("POST", s.cfg.NtfyURL, strings.NewReader(message))
	if err != nil {
		s.logger.Error("failed to create ntfy request", "error", err)
		return
	}
	req.Header.Set("Title", title)
	req.Header.Set("Priority", priority)
	req.Header.Set("Tags", tags)
	req.Header.Set("Click", clickURL)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		s.logger.Error("ntfy request failed", "error", err)
		return
	}
	resp.Body.Close()

	s.logger.Info("ntfy notification sent", "title", title, "click", clickURL)
}

// sendStopNotification sends a session completion notification.
func (s *Server) sendStopNotification(sess *store.Session, mins int) {
	if s.cfg.NtfyURL == "" {
		return
	}

	title := fmt.Sprintf("[%s] Session complete", sess.Project)
	body := fmt.Sprintf("Finished after %dm", mins)

	req, err := http.NewRequest("POST", s.cfg.NtfyURL, strings.NewReader(body))
	if err != nil {
		s.logger.Error("failed to create ntfy request", "error", err)
		return
	}
	req.Header.Set("Title", title)
	req.Header.Set("Priority", "default")
	req.Header.Set("Tags", "white_check_mark")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		s.logger.Error("ntfy request failed", "error", err)
		return
	}
	resp.Body.Close()

	s.logger.Info("stop notification sent", "project", sess.Project, "duration_min", mins)
}
