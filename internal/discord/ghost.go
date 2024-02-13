// Package discord contains utility code used to make automated posts to Discord
// channels using a webhook URL
package discord

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"
)

// PostGhostAlert makes an HTTP request to the given Discord webhook in order to post an
// image alert that has just been submitted by the given user, with the provided
// description and image URL
func PostGhostAlert(webhookUrl, submitterUsername, description, imageUrl string) error {
	// Parse the filename of the image that we want to download from our own storage and
	// upload to Discord along with our message
	imageFilename := imageUrl
	slashPos := strings.LastIndex(imageUrl, "/")
	if slashPos >= 0 {
		imageFilename = imageUrl[slashPos+1:]
	}

	// Open an HTTP connection to download the image from storage
	imageReq, err := http.NewRequest(http.MethodGet, imageUrl, nil)
	if err != nil {
		return fmt.Errorf("failed to create request to GET %s: %w", imageUrl, err)
	}
	imageRes, err := http.DefaultClient.Do(imageReq)
	if err != nil {
		return fmt.Errorf("GET %s failed: %w", imageUrl, err)
	}
	if imageRes.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s failed with status %d", imageUrl, err)
	}
	imageContentType := imageRes.Header.Get("content-type")
	if !strings.HasPrefix(imageContentType, "image/") {
		return fmt.Errorf("GET %s returned unexpected content-type '%s'", imageUrl, imageContentType)
	}

	// Prepare a JSON payload (which we'll encode as a multipart/form-data section
	// titled "payload_json") to describe the message we want to post to Discord
	payload := discordWebhookPayload{
		Content: fmt.Sprintf("Ghost from **%s**: _%s_", submitterUsername, description),
		Attachments: []discordWebhookAttachment{
			{
				Id:          0,
				Description: description,
				Filename:    imageFilename,
			},
		},
	}

	// Prepare a multipart/form-data writer so we can upload that file, streamed
	// directly from our HTTP client connection, after first including the JSON payload
	var b bytes.Buffer
	w := multipart.NewWriter(&b)

	// First part: payload_json, describing the text of the message etc.
	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", `form-data; name="payload_json"`)
	h.Set("Content-Type", "application/json")
	w.CreatePart(h)
	if err := json.NewEncoder(&b).Encode(payload); err != nil {
		return fmt.Errorf("failed to encode payload_json: %w", err)
	}

	// Second part: the image data for the file we want to include with the message, as
	// 'image/jpeg' etc.
	h = make(textproto.MIMEHeader)
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="files[0]"; filename="%s"`, imageFilename))
	h.Set("Content-Type", imageContentType)
	w.CreatePart(h)
	if _, err := io.Copy(&b, imageRes.Body); err != nil {
		return fmt.Errorf("failed to copy image data from %s to request body: %v", imageUrl, err)
	}
	w.Close()

	// Make the request to our Discord webhook URL
	req, err := http.NewRequest(http.MethodPost, webhookUrl, &b)
	if err != nil {
		return err
	}
	req.Header.Set("content-type", w.FormDataContentType())
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	if res.StatusCode < 200 || res.StatusCode > 299 {
		suffix := ""
		body, err := io.ReadAll(res.Body)
		if err == nil {
			suffix = fmt.Sprintf(": %s", body)
		}
		return fmt.Errorf("got %d response from Discord webhook%s", res.StatusCode, suffix)
	}
	return nil

}

type discordWebhookPayload struct {
	Content     string                     `json:"content"`
	Attachments []discordWebhookAttachment `json:"attachments"`
}

type discordWebhookAttachment struct {
	Id          int    `json:"id"`
	Description string `json:"description"`
	Filename    string `json:"filename"`
}
