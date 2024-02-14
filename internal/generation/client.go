package generation

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image/jpeg"
	"image/png"
	"net/http"

	openai "github.com/sashabaranov/go-openai"
)

type ImageType string

const (
	ImageTypeScreen      ImageType = "screen"
	ImageTypeTransparent ImageType = "transparent"
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
	Type        ImageType
	ContentType string
	Data        []byte
}

type Client interface {
	GenerateImage(ctx context.Context, prompt string, opaqueUserId string, imageType ImageType) (*Image, error)
}

type client struct {
	c *openai.Client
}

func NewClient(openaiToken string) Client {
	return &client{
		c: openai.NewClient(openaiToken),
	}
}

func (c *client) GenerateImage(ctx context.Context, prompt string, opaqueUserId string, imageType ImageType) (*Image, error) {
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

	// The response from OpenAI should include a URL to our newly-generated image, in
	// PNG format: fetch that image and process it according to our desired image type
	// (i.e. convert to JPEG for a high-detail image that will be rendered with a screen
	// blending mode; chroma-key and save as a PNG for images that should have a
	// transparent background)
	return fetchImageData(ctx, result.URL, imageType)
}

// fetchImageData downloads the image at the given URL, converting it from PNG to JPEG
func fetchImageData(ctx context.Context, url string, imageType ImageType) (*Image, error) {
	// Download the OpenAI-hosted PNG image so we can store it permanently
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("got status %d from request for OpenAI-hosted image", res.StatusCode)
	}

	// Verify that OpenAI has linked us to a .png
	contentType := res.Header.Get("content-type")
	if contentType != "image/png" {
		return nil, fmt.Errorf("got unexpected content-type '%s' for OpenAI-hosted image", contentType)
	}

	// Decode the PNG, reading it directly from the response body
	bmpData, err := png.Decode(res.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to decode PNG data for OpenAI-hosted image: %w", err)
	}

	// Preallocate a buffer that's roughly as large as the largest 1024x1024 JPEG
	// we can reasonably expect to produce, then write our compressed JPEG data into
	// it
	jpegBuffer := bytes.NewBuffer(make([]byte, 0, 512*1024))
	if err := jpeg.Encode(jpegBuffer, bmpData, &jpeg.Options{Quality: 80}); err != nil {
		return nil, fmt.Errorf("failed to encode JPEG image from decoded PNG image: %w", err)
	}

	// Return the JPEG data
	return &Image{
		ContentType: "image/jpeg",
		Data:        jpegBuffer.Bytes(),
	}, nil
}
