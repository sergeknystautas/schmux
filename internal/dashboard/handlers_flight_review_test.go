package dashboard

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/sergeknystautas/schmux/internal/flightreview"
	"github.com/sergeknystautas/schmux/internal/schmuxdir"
)

// reviewRouter mounts only the flight routes, with the given review service.
func reviewRouter(e *flightEnv, review *flightreview.Service) http.Handler {
	h := &FlightHandlers{manager: e.mgr, state: e.st, logger: e.server.logger, review: review}
	r := chi.NewRouter()
	r.Route("/api", func(r chi.Router) {
		r.Group(func(r chi.Router) {
			r.Use(e.server.csrfMiddleware)
			h.registerMutatingRoutes(r)
		})
	})
	return r
}

func postReview(t *testing.T, router http.Handler, query, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/flights/review"+query, strings.NewReader(body))
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	return rr
}

func reviewService(e *flightEnv, attach bool) *flightreview.Service {
	svc := flightreview.NewService(flightreview.Options{State: e.st, Now: func() time.Time { return time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC) }})
	if attach {
		svc.SetFlights(e.mgr)
	}
	return svc
}

func TestFlightReviewHandlerReturnsSelfContainedHTML(t *testing.T) {
	schmuxdir.Set(t.TempDir())
	t.Cleanup(func() { schmuxdir.Set("") })
	e := newFlightEnv(t)
	router := reviewRouter(e, reviewService(e, true))

	rr := postReview(t, router, "?since=2026-10-01&until=2026-10-31&cohort=activity", "## Needs data\n- c-carried: still open\n")

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rr.Code, rr.Body.String())
	}
	if got := rr.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Errorf("Content-Type = %q", got)
	}
	if got := rr.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q", got)
	}
	if got := rr.Header().Get("X-Schmux-Flight-Review"); got != "1" {
		t.Errorf("X-Schmux-Flight-Review = %q", got)
	}
	body := rr.Body.String()
	for _, want := range []string{"<!DOCTYPE html>", "Flight retrospective", `id="data-quality"`, "@media print", "c-carried"} {
		if !strings.Contains(body, want) {
			t.Errorf("response lacks %q", want)
		}
	}
	for _, forbidden := range []string{"<script", "<link", "http://", "https://"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("response contains %q; the report must be self-contained", forbidden)
		}
	}
}

func TestFlightReviewHandlerRejectsInvalidRequest(t *testing.T) {
	schmuxdir.Set(t.TempDir())
	t.Cleanup(func() { schmuxdir.Set("") })
	e := newFlightEnv(t)
	router := reviewRouter(e, reviewService(e, true))

	for name, query := range map[string]string{
		"no parameters":      "",
		"missing cohort":     "?since=2026-10-01&until=2026-10-31",
		"unknown cohort":     "?since=2026-10-01&until=2026-10-31&cohort=everything",
		"bad since":          "?since=yesterday&until=2026-10-31&cohort=disposed",
		"bad until":          "?since=2026-10-01&until=2026-13-45&cohort=disposed",
		"until before since": "?since=2026-10-31&until=2026-10-01&cohort=disposed",
	} {
		t.Run(name, func(t *testing.T) {
			rr := postReview(t, router, query, "")
			if rr.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400: %s", rr.Code, rr.Body.String())
			}
			if got := rr.Header().Get("Content-Type"); got != "application/json" {
				t.Errorf("Content-Type = %q, want the dashboard JSON error shape", got)
			}
			if !strings.Contains(rr.Body.String(), `"error"`) {
				t.Errorf("body = %s", rr.Body.String())
			}
		})
	}
}

func TestParseReviewDateUsesDaemonLocalTime(t *testing.T) {
	oldLocal := time.Local
	defer func() { time.Local = oldLocal }()
	time.Local = time.FixedZone("review", -7*60*60)

	got, err := parseReviewDate("2026-10-01")

	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 10, 1, 0, 0, 0, 0, time.Local)
	if !got.Equal(want) || got.Sub(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)) != 7*time.Hour {
		t.Fatalf("date = %s, want local midnight at UTC-7", got)
	}
}

func TestFlightReviewHandlerUnavailableWithoutService(t *testing.T) {
	e := newFlightEnv(t)
	router := reviewRouter(e, nil)

	rr := postReview(t, router, "?since=2026-10-01&until=2026-10-31&cohort=disposed", "")

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `"error"`) {
		t.Errorf("body = %s", rr.Body.String())
	}
}

func TestFlightReviewHandlerReportsAServiceFailureAsJSON(t *testing.T) {
	e := newFlightEnv(t)
	router := reviewRouter(e, reviewService(e, false)) // no flights manager attached

	rr := postReview(t, router, "?since=2026-10-01&until=2026-10-31&cohort=disposed", "")

	if rr.Code != http.StatusInternalServerError || !strings.Contains(rr.Body.String(), `"error"`) {
		t.Fatalf("status = %d body = %s, want a 500 JSON error", rr.Code, rr.Body.String())
	}
}
