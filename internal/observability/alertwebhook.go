package observability

import (
	"context"
	"crypto/subtle"
	"log/slog"
	"net/http"

	"github.com/kyledickey/shed/internal/grafana"
)

// AlertWebhookPath is the path of the alert webhook on the gateway's host
// loopback listener.
const AlertWebhookPath = "/shed/grafana-alerts"

// alertWebhookLimit bounds the body of a webhook call.
const alertWebhookLimit = 1 << 20

// alertWebhook receives Grafana's calls to its contact point. A failure to
// handle the alerts returns 500 so that Grafana sends them again.
func alertWebhook(
	token func(ctx context.Context) (string, error),
	handle func(ctx context.Context, alerts []grafana.WebhookAlert) error,
	log *slog.Logger,
) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		ctx := r.Context()
		want, err := token(ctx)
		if err != nil {
			log.Error("alert webhook: load token", "err", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		got := r.Header.Get("Authorization")
		if want == "" || subtle.ConstantTimeCompare([]byte(got), []byte("Bearer "+want)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		alerts, err := grafana.ParseWebhook(http.MaxBytesReader(w, r.Body, alertWebhookLimit))
		if err != nil {
			log.Warn("alert webhook: bad request", "err", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if err := handle(ctx, alerts); err != nil {
			log.Error("alert webhook: handle alerts", "err", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}
