package private

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
)

type Request struct {
	AppID       string
	PackageID   string
	Name        string
	Summary     string
	Version     string
	Changelog   string
	DownloadURL string
	SHA256      string
}

type Result struct {
	Published     bool   `json:"published"`
	Skipped       bool   `json:"skipped"`
	Created       bool   `json:"created"`
	Existing      bool   `json:"existing"`
	AppID         string `json:"appId"`
	VersionID     string `json:"versionId"`
	PackageID     string `json:"packageId"`
	Version       string `json:"version"`
	OnlineVersion string `json:"onlineVersion,omitempty"`
	SkipReason    string `json:"skipReason,omitempty"`
	DownloadURL   string `json:"downloadUrl"`
	SHA256        string `json:"sha256"`
}

type identifier string

func (id *identifier) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		*id = ""
		return nil
	}
	if data[0] == '"' {
		var value string
		if err := json.Unmarshal(data, &value); err != nil {
			return err
		}
		*id = identifier(strings.TrimSpace(value))
		return nil
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value json.Number
	if err := decoder.Decode(&value); err != nil {
		return errors.New("invalid identifier")
	}
	*id = identifier(value.String())
	return nil
}

type versionDTO struct {
	ID          identifier `json:"id"`
	AppID       identifier `json:"appId"`
	Version     string     `json:"version"`
	DownloadURL string     `json:"downloadUrl"`
	SHA256      string     `json:"sha256"`
}

type appDTO struct {
	ID            identifier   `json:"id"`
	PackageID     string       `json:"packageId"`
	Name          string       `json:"name"`
	CanUpload     bool         `json:"canUploadVersion"`
	LatestVersion *versionDTO  `json:"latestVersion"`
	Versions      []versionDTO `json:"versions"`
}

type createApplicationRequest struct {
	PackageID   string `json:"packageId"`
	Name        string `json:"name"`
	Summary     string `json:"summary"`
	Version     string `json:"version"`
	Changelog   string `json:"changelog,omitempty"`
	SourceType  string `json:"sourceType"`
	DownloadURL string `json:"downloadUrl"`
	SHA256      string `json:"sha256"`
}

type createVersionRequest struct {
	Version     string `json:"version"`
	Changelog   string `json:"changelog,omitempty"`
	SourceType  string `json:"sourceType"`
	DownloadURL string `json:"downloadUrl"`
	SHA256      string `json:"sha256"`
}
