package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

var (
	apiBase    = "https://www.googleapis.com/drive/v3"
	uploadBase = "https://www.googleapis.com/upload/drive/v3"
)

const (
	fileFields = "id,name,mimeType,parents,md5Checksum,modifiedTime,size"
	folderMime = "application/vnd.google-apps.folder"
)

type File struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	MimeType     string    `json:"mimeType"`
	Parents      []string  `json:"parents"`
	MD5          string    `json:"md5Checksum"`
	ModifiedTime time.Time `json:"modifiedTime"`
	Size         int64     `json:"size,string"`
}

func (f *File) IsFolder() bool {
	return f.MimeType == folderMime
}

type Drive struct {
	tokens *tokenSource
	client *http.Client
}

func newDrive() (*Drive, error) {
	creds, err := loadCredentials()
	if err != nil {
		return nil, err
	}
	return &Drive{tokens: &tokenSource{creds: *creds}, client: &http.Client{}}, nil
}

func (d *Drive) send(req *http.Request) (*http.Response, error) {
	token, err := d.tokens.token(req.Context())
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := d.client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		defer resp.Body.Close()
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("%s %s: %s: %s", req.Method, req.URL.Path, resp.Status, strings.TrimSpace(string(body)))
	}
	return resp, nil
}

func (d *Drive) call(ctx context.Context, method, target string, payload, out any) error {
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return err
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json; charset=UTF-8")
	}
	resp, err := d.send(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if out == nil {
		_, err = io.Copy(io.Discard, resp.Body)
		return err
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (d *Drive) RootID(ctx context.Context) (string, error) {
	var root File
	err := d.call(ctx, http.MethodGet, apiBase+"/files/root?fields=id", nil, &root)
	return root.ID, err
}

func (d *Drive) list(ctx context.Context, query string) ([]File, error) {
	var all []File
	pageToken := ""
	for {
		params := url.Values{
			"q":        {query},
			"pageSize": {"1000"},
			"spaces":   {"drive"},
			"fields":   {"nextPageToken,files(" + fileFields + ")"},
		}
		if pageToken != "" {
			params.Set("pageToken", pageToken)
		}
		var page struct {
			NextPageToken string `json:"nextPageToken"`
			Files         []File `json:"files"`
		}
		if err := d.call(ctx, http.MethodGet, apiBase+"/files?"+params.Encode(), nil, &page); err != nil {
			return nil, err
		}
		all = append(all, page.Files...)
		if page.NextPageToken == "" {
			return all, nil
		}
		pageToken = page.NextPageToken
	}
}

func (d *Drive) ListAll(ctx context.Context) ([]File, error) {
	return d.list(ctx, "trashed = false")
}

func (d *Drive) ListFolders(ctx context.Context, parentID string) ([]File, error) {
	return d.list(ctx, fmt.Sprintf("'%s' in parents and mimeType = '%s' and trashed = false", parentID, folderMime))
}

func (d *Drive) StartPageToken(ctx context.Context) (string, error) {
	var result struct {
		StartPageToken string `json:"startPageToken"`
	}
	err := d.call(ctx, http.MethodGet, apiBase+"/changes/startPageToken", nil, &result)
	return result.StartPageToken, err
}

func (d *Drive) Changes(ctx context.Context, pageToken string) (bool, string, error) {
	changed := false
	for {
		params := url.Values{
			"pageToken": {pageToken},
			"pageSize":  {"1000"},
			"spaces":    {"drive"},
			"fields":    {"nextPageToken,newStartPageToken,changes(fileId)"},
		}
		var page struct {
			NextPageToken     string `json:"nextPageToken"`
			NewStartPageToken string `json:"newStartPageToken"`
			Changes           []struct {
				FileID string `json:"fileId"`
			} `json:"changes"`
		}
		if err := d.call(ctx, http.MethodGet, apiBase+"/changes?"+params.Encode(), nil, &page); err != nil {
			return false, "", err
		}
		if len(page.Changes) > 0 {
			changed = true
		}
		if page.NewStartPageToken != "" {
			return changed, page.NewStartPageToken, nil
		}
		if page.NextPageToken == "" {
			return false, "", errors.New("changes response without a page token")
		}
		pageToken = page.NextPageToken
	}
}

func (d *Drive) Download(ctx context.Context, id, dest string, beforeReplace func() error) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiBase+"/files/"+url.PathEscape(id)+"?alt=media", nil)
	if err != nil {
		return err
	}
	resp, err := d.send(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	tmp, err := os.CreateTemp(filepath.Dir(dest), tempPrefix+"download-*")
	if err != nil {
		return err
	}
	_, err = io.Copy(tmp, resp.Body)
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Chmod(tmp.Name(), 0o644)
	}
	if err == nil {
		err = beforeReplace()
	}
	if err == nil {
		err = os.Rename(tmp.Name(), dest)
	}
	if err != nil {
		os.Remove(tmp.Name())
	}
	return err
}

func (d *Drive) CreateFolder(ctx context.Context, name, parentID string) (*File, error) {
	var folder File
	payload := map[string]any{"name": name, "mimeType": folderMime, "parents": []string{parentID}}
	err := d.call(ctx, http.MethodPost, apiBase+"/files?fields="+url.QueryEscape(fileFields), payload, &folder)
	return &folder, err
}

func (d *Drive) Upload(ctx context.Context, fileID, name, parentID, localPath string, modified time.Time) (*File, error) {
	source, err := os.Open(localPath)
	if err != nil {
		return nil, err
	}
	defer source.Close()
	info, err := source.Stat()
	if err != nil {
		return nil, err
	}

	metadata := map[string]any{"modifiedTime": modified.UTC().Format(time.RFC3339Nano)}
	method, target := http.MethodPatch, uploadBase+"/files/"+url.PathEscape(fileID)
	if fileID == "" {
		method, target = http.MethodPost, uploadBase+"/files"
		metadata["name"] = name
		metadata["parents"] = []string{parentID}
	}
	target += "?uploadType=resumable&fields=" + url.QueryEscape(fileFields)
	data, err := json.Marshal(metadata)
	if err != nil {
		return nil, err
	}
	start, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	start.Header.Set("Content-Type", "application/json; charset=UTF-8")
	start.Header.Set("X-Upload-Content-Length", strconv.FormatInt(info.Size(), 10))
	resp, err := d.send(start)
	if err != nil {
		return nil, err
	}
	resp.Body.Close()
	session := resp.Header.Get("Location")
	if session == "" {
		return nil, errors.New("upload session without a Location header")
	}

	put, err := http.NewRequestWithContext(ctx, http.MethodPut, session, source)
	if err != nil {
		return nil, err
	}
	put.ContentLength = info.Size()
	if info.Size() == 0 {
		put.Body = http.NoBody
	}
	resp, err = d.send(put)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var uploaded File
	if err := json.NewDecoder(resp.Body).Decode(&uploaded); err != nil {
		return nil, err
	}
	return &uploaded, nil
}

func (d *Drive) Trash(ctx context.Context, id string) error {
	return d.call(ctx, http.MethodPatch, apiBase+"/files/"+url.PathEscape(id)+"?fields=id", map[string]any{"trashed": true}, nil)
}
