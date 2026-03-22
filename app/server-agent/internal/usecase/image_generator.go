package usecase

import "context"

// ImageGenerator generates an image from a text prompt and returns PNG bytes.
type ImageGenerator interface {
	GenerateImage(ctx context.Context, prompt string) ([]byte, error)
}
