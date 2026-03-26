package campaigns

import (
	"context"

	"github.com/autosdk/ppp/server-agent/internal/appport"
	"github.com/autosdk/ppp/server-agent/internal/projection"
)

type TaskControl = appport.TaskControl

type CaptionGenerator interface {
	GenerateCaption(ctx context.Context, prompt string) (string, error)
	GenerateCaptionStream(ctx context.Context, prompt string, onChunk func(string)) error
}

type ImageGenerator interface {
	GenerateImage(ctx context.Context, prompt string) ([]byte, error)
}

type AdbFilePusher = appport.AdbFilePusher

type ProjectionPublisher = projection.Publisher

type ProjectionEvent = projection.Event

type CreateTaskRequest = appport.CreateTaskRequest

var pollTaskUntilTerminal = appport.PollTaskUntilTerminal
