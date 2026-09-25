package dashboard

import (
	"encoding/json"
	"net/http"
)

type chatLoadTelemetry struct {
	SessionID       string                  `json:"sessionId"`
	LoadID          string                  `json:"loadId"`
	At              string                  `json:"at"`
	Start           string                  `json:"start"`
	FrameChars      int                     `json:"frameChars"`
	Records         int                     `json:"records"`
	RouteToSocketMs float64                 `json:"routeToSocketMs"`
	SocketOpenMs    float64                 `json:"socketOpenMs"`
	HistoryWaitMs   float64                 `json:"historyWaitMs"`
	ParseMs         float64                 `json:"parseMs"`
	ResolveMs       *float64                `json:"resolveMs,omitempty"`
	ReduceMs        float64                 `json:"reduceMs"`
	CommitMs        float64                 `json:"commitMs"`
	AfterPaintMs    float64                 `json:"afterPaintMs"`
	TotalMs         float64                 `json:"totalMs"`
	Reduction       *chatReductionTelemetry `json:"reduction,omitempty"`
}

type chatReductionTelemetry struct {
	Categories []struct {
		Category   string  `json:"category"`
		Records    int     `json:"records"`
		DurationMs float64 `json:"durationMs"`
		MaxMs      float64 `json:"maxMs"`
	} `json:"categories"`
	ProbeOverheadMs float64 `json:"probeOverheadMs"`
	Items           int     `json:"items"`
	Turns           int     `json:"turns"`
	Segments        int     `json:"segments"`
	Images          int     `json:"images"`
	Operations      int     `json:"operations"`
}

type chatImageLoadTelemetry struct {
	Path           string   `json:"path"`
	At             string   `json:"at"`
	ResourceMs     *float64 `json:"resourceMs"`
	LoadMs         *float64 `json:"loadMs"`
	TransferBytes  *int64   `json:"transferBytes"`
	DecodedBytes   *int64   `json:"decodedBytes"`
	Width          int      `json:"width"`
	Height         int      `json:"height"`
	TTFBMs         *float64 `json:"ttfbMs"`
	DownloadMs     *float64 `json:"downloadMs"`
	PostResponseMs *float64 `json:"postResponseMs"`
	Error          bool     `json:"error"`
}

func (s *Server) handleChatTelemetry(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Loads  []chatLoadTelemetry      `json:"loads"`
		Images []chatImageLoadTelemetry `json:"images"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024)).Decode(&req); err != nil {
		writeJSONError(w, "invalid chat telemetry", http.StatusBadRequest)
		return
	}
	if len(req.Loads) > 20 || len(req.Images) > 20 {
		writeJSONError(w, "too many chat telemetry samples", http.StatusBadRequest)
		return
	}
	for _, sample := range req.Loads {
		fields := []any{
			"session", sample.SessionID, "at", sample.At, "start", sample.Start,
			"load_id", sample.LoadID,
			"frame_chars", sample.FrameChars, "records", sample.Records,
			"route_to_socket_ms", sample.RouteToSocketMs, "socket_open_ms", sample.SocketOpenMs,
			"history_wait_ms", sample.HistoryWaitMs, "parse_ms", sample.ParseMs,
			"reduce_ms", sample.ReduceMs, "commit_ms", sample.CommitMs,
			"after_paint_ms", sample.AfterPaintMs, "total_ms", sample.TotalMs,
		}
		if s.config.GetChatLoadProfilingEnabled() && sample.Reduction != nil {
			fields = append(fields, "resolve_ms", telemetryValue(sample.ResolveMs), "reduction", sample.Reduction)
		}
		s.recordChatPerformance("browser_load", fields)
	}
	for _, sample := range req.Images {
		fields := []any{
			"path", sample.Path, "at", sample.At,
			"resource_ms", telemetryValue(sample.ResourceMs), "load_ms", telemetryValue(sample.LoadMs),
			"transfer_bytes", telemetryValue(sample.TransferBytes), "decoded_bytes", telemetryValue(sample.DecodedBytes),
			"width", sample.Width, "height", sample.Height,
		}
		if s.config.GetChatLoadProfilingEnabled() {
			fields = append(fields,
				"ttfb_ms", telemetryValue(sample.TTFBMs),
				"download_ms", telemetryValue(sample.DownloadMs),
				"post_response_ms", telemetryValue(sample.PostResponseMs),
				"error", sample.Error)
		}
		s.recordChatPerformance("browser_image", fields)
	}
	w.WriteHeader(http.StatusNoContent)
}

func telemetryValue[T any](value *T) any {
	if value == nil {
		return nil
	}
	return *value
}
