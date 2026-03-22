package usecase

import "context"

// CaptionGenerator generates Instagram caption text from a prompt.
type CaptionGenerator interface {
	GenerateCaption(ctx context.Context, prompt string) (string, error)
}
