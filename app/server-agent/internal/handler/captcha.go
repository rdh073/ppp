package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/autosdk/ppp/server-agent/internal/tools/llm"
)

// CaptchaHandler exposes a synchronous captcha-solve endpoint.
// It is stateless: each request is fully self-contained.
//
//	POST /captcha/solve
//	  Request:  { "imageBase64": "<png base64>", "gridBounds": [l,t,r,b], "cols": 4 }
//	  Response: { "taps": ["320,480","640,480"], "count": 2 }
type CaptchaHandler struct {
	vision llm.VisionModelClient
	log    *slog.Logger
}

// NewCaptchaHandler creates a CaptchaHandler. If vision is nil the handler
// returns 503 on all solve requests (captcha solving is disabled).
func NewCaptchaHandler(vision llm.VisionModelClient, log *slog.Logger) *CaptchaHandler {
	return &CaptchaHandler{vision: vision, log: log}
}

func (h *CaptchaHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	sub := strings.TrimPrefix(r.URL.Path, "/captcha")
	sub = strings.TrimPrefix(sub, "/")
	switch {
	case r.Method == http.MethodPost && sub == "solve":
		h.solve(w, r)
	default:
		http.Error(w, "not found", http.StatusNotFound)
	}
}

// ---- request / response types ----

type captchaSolveRequest struct {
	ImageBase64 string `json:"imageBase64"`
	GridBounds  []int  `json:"gridBounds,omitempty"` // [left,top,right,bottom]; overrides LLM estimate
	Cols        int    `json:"cols,omitempty"`        // grid columns; overrides LLM estimate; default 4
}

type captchaSolveResponse struct {
	Taps  []string `json:"taps"`
	Count int      `json:"count"`
}

func (h *CaptchaHandler) solve(w http.ResponseWriter, r *http.Request) {
	if h.vision == nil {
		http.Error(w, "captcha solver disabled: vision model not configured", http.StatusServiceUnavailable)
		return
	}

	var req captchaSolveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.ImageBase64) == "" {
		http.Error(w, "imageBase64 is required", http.StatusBadRequest)
		return
	}

	taps, err := solveCaptcha(r.Context(), h.vision, req.ImageBase64, req.GridBounds, req.Cols)
	if err != nil {
		h.log.Error("captcha solve failed", "err", err)
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(captchaSolveResponse{
		Taps:  taps,
		Count: len(taps),
	})
}

// ---- core logic (separately testable) ----

// captchaVisionOutput is the structured output the LLM must produce.
type captchaVisionOutput struct {
	Squares    []int  `json:"squares"`    // 1-indexed square numbers to tap
	GridBounds [4]int `json:"gridBounds"` // [left,top,right,bottom] inferred from image
	Cols       int    `json:"cols"`       // grid column count (rows == cols)
}

var captchaOutputSchema = json.RawMessage(`{
	"type": "object",
	"required": ["squares","gridBounds","cols"],
	"properties": {
		"squares":    {"type":"array","items":{"type":"integer"}},
		"gridBounds": {"type":"array","items":{"type":"integer"},"minItems":4,"maxItems":4},
		"cols":       {"type":"integer","minimum":1}
	}
}`)

const captchaPrompt = `You are analysing a CAPTCHA on an Android screen.
1. Read the instruction text visible in the image (e.g. "Select all traffic lights").
2. Find the grid of image tiles and determine its pixel bounding box [left, top, right, bottom] in screen coordinates.
3. Count the number of columns in the grid (usually 3 or 4). Rows equal columns.
4. Identify which 1-indexed tiles (numbered left-to-right, top-to-bottom starting at 1) match the instruction.
Return ONLY a JSON object matching the output schema. Do not add any explanation.`

// solveCaptcha calls the vision LLM once to detect the captcha instruction,
// locate the grid, and identify matching squares, then converts squares to
// absolute tap coordinates.
//
// callerBounds and callerCols override the LLM's inferred values when provided
// (caller values are more precise if the workflow already knows the grid bounds).
func solveCaptcha(
	ctx context.Context,
	client llm.VisionModelClient,
	imageBase64 string,
	callerBounds []int,
	callerCols int,
) ([]string, error) {
	raw, err := client.AnalyzeImage(ctx, llm.VisionAnalyzeRequest{
		ToolName:     "captcha.solve",
		ImageBase64:  imageBase64,
		MimeType:     "image/png",
		Prompt:       captchaPrompt,
		OutputSchema: captchaOutputSchema,
	})
	if err != nil {
		return nil, fmt.Errorf("vision call: %w", err)
	}

	var out captchaVisionOutput
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode vision output: %w", err)
	}
	if len(out.Squares) == 0 {
		return []string{}, nil
	}

	// Prefer caller-supplied values when present.
	bounds := out.GridBounds
	if len(callerBounds) == 4 {
		bounds = [4]int{callerBounds[0], callerBounds[1], callerBounds[2], callerBounds[3]}
	}
	cols := out.Cols
	if callerCols > 0 {
		cols = callerCols
	}
	if cols <= 0 {
		cols = 4
	}

	return squaresToTapCoords(out.Squares, bounds, cols), nil
}

// squaresToTapCoords converts 1-indexed grid square numbers to absolute screen
// tap coordinates ("x,y" strings). Same geometry as builtin_captcha.go.
// Duplicated here intentionally: handler must not import the tools package.
func squaresToTapCoords(squares []int, bounds [4]int, cols int) []string {
	rows := cols
	cellW := float64(bounds[2]-bounds[0]) / float64(cols)
	cellH := float64(bounds[3]-bounds[1]) / float64(rows)
	taps := make([]string, 0, len(squares))
	for _, sq := range squares {
		if sq < 1 {
			continue
		}
		col := (sq - 1) % cols
		row := (sq - 1) / cols
		cx := float64(bounds[0]) + float64(col)*cellW + cellW/2.0
		cy := float64(bounds[1]) + float64(row)*cellH + cellH/2.0
		taps = append(taps, fmt.Sprintf("%d,%d", int(cx), int(cy)))
	}
	return taps
}
