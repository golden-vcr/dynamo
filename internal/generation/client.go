package generation

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"

	openai "github.com/sashabaranov/go-openai"
)

// ErrRejected is returned when the image generation API rejected the request to
// generate one or more images, typically because the prompt contained text that was
// classified as objectionable
var ErrRejected = errors.New("image generation request rejected")

// rejectionError unwraps to ErrRejected and carries the original client-facing message
// returned as a 400 response from the image generation API
type rejectionError struct {
	message string
}

// Error formats a rejection error, prefixed with the ErrRejected message and including
// the original error message received from the image generation API
func (e *rejectionError) Error() string {
	return fmt.Sprintf("%v: %s", ErrRejected, e.message)
}

// Unwrap identifies a value of this type as synonymous with ErrRejected
func (e *rejectionError) Unwrap() error {
	return ErrRejected
}

type Image struct {
	ContentType string
	Data        []byte
}

type Client interface {
	GenerateText(ctx context.Context, prompt string, opaqueUserId string) (string, error)
	GenerateImage(ctx context.Context, prompt string, opaqueUserId string) (*Image, error)
}

type client struct {
	c *openai.Client
}

func NewClient(openaiToken string) Client {
	return &client{
		c: openai.NewClient(openaiToken),
	}
}

func (c *client) GenerateText(ctx context.Context, prompt string, opaqueUserId string) (string, error) {
	res, err := c.c.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model: "gpt-3.5-turbo-0125",
		Messages: []openai.ChatCompletionMessage{
			{
				Role:    openai.ChatMessageRoleUser,
				Content: prompt,
			},
		},
		N:    1,
		User: opaqueUserId,
	})
	if err != nil {
		// If our request was rejected with a 400 error, return ErrRejected so the
		// caller can propagate it as a client-level error
		apiError := &openai.APIError{}
		if errors.As(err, &apiError) && apiError.HTTPStatusCode == http.StatusBadRequest && apiError.Type == "invalid_request_error" {
			return "", &rejectionError{apiError.Message}
		}
		return "", err
	}

	// If we didn't get exactly one image, abort
	numResultChoices := len(res.Choices)
	if numResultChoices != 1 {
		return "", fmt.Errorf("expected 1 or more result choices from OpenAI; got %d", numResultChoices)
	}
	result := res.Choices[0].Message.Content
	if result == "" {
		return "", fmt.Errorf("go no text from OpenAI response choice")
	}
	return result, nil
}

func (c *client) GenerateImage(ctx context.Context, prompt string, opaqueUserId string) (*Image, error) {
	// Send a request to the OpenAI API to generate an image from our prompt: this
	// request will block until the image is ready
	res, err := c.c.CreateImage(ctx, openai.ImageRequest{
		Prompt:         prompt,
		Model:          openai.CreateImageModelDallE3,
		N:              1,
		Quality:        openai.CreateImageQualityStandard,
		Size:           openai.CreateImageSize1024x1024,
		Style:          openai.CreateImageStyleVivid,
		ResponseFormat: openai.CreateImageResponseFormatURL,
		User:           opaqueUserId,
	})
	if err != nil {
		// If our request was rejected with a 400 error, return ErrRejected so the
		// caller can propagate it as a client-level error
		apiError := &openai.APIError{}
		if errors.As(err, &apiError) && apiError.HTTPStatusCode == http.StatusBadRequest && apiError.Type == "invalid_request_error" {
			return nil, &rejectionError{apiError.Message}
		}
		return nil, err
	}

	// If we didn't get exactly one image, abort
	numResultImages := len(res.Data)
	if numResultImages != 1 {
		return nil, fmt.Errorf("expected 1 result image from OpenAI; got %d", numResultImages)
	}
	result := res.Data[0]

	// Download the OpenAI-hosted PNG image so we can store it permanently
	pngReq, err := http.NewRequestWithContext(ctx, http.MethodGet, result.URL, nil)
	if err != nil {
		return nil, err
	}
	pngRes, err := http.DefaultClient.Do(pngReq)
	if err != nil {
		return nil, err
	}
	if pngRes.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("got status %d from request for OpenAI-hosted image", pngRes.StatusCode)
	}

	// Verify that OpenAI has linked us to a .png
	contentType := pngRes.Header.Get("content-type")
	if contentType != "image/png" {
		return nil, fmt.Errorf("got unexpected content-type '%s' for OpenAI-hosted image", contentType)
	}

	// Return the PNG data
	pngData, err := io.ReadAll(pngRes.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read PNG image data from OpenAI response body: %w", err)
	}
	return &Image{
		ContentType: contentType,
		Data:        pngData,
	}, nil
}
