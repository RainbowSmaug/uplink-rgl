package apollo

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
)

type App struct {
	UUID     string `json:"uuid,omitempty"`
	Name     string `json:"name"`
	ImageURL string `json:"image-path"`
	Cmd      string `json:"cmd"`
}

type Client struct {
	BaseURL    string
	Username   string
	Password   string
	httpClient *http.Client
}

func NewClient(baseURL, username, password string) *Client {
	jar, _ := cookiejar.New(nil)

	httpClient := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
		Jar: jar,
	}

	return &Client{
		BaseURL:    baseURL,
		Username:   username,
		Password:   password,
		httpClient: httpClient,
	}
}

func (c *Client) Login() error {
	payload := map[string]string{
		"username": c.Username,
		"password": c.Password,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	resp, err := c.httpClient.Post(c.BaseURL+"/api/login", "application/json", bytes.NewBuffer(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("login failed: %d", resp.StatusCode)
	}

	return nil
}

func (c *Client) GetApps() ([]App, error) {
	req, err := http.NewRequest("GET", c.BaseURL+"/api/apps", nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	var result struct {
		Apps []App `json:"apps"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return result.Apps, nil
}

func (c *Client) AddApp(app App) error {
	body, err := json.Marshal(app)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("POST", c.BaseURL+"/api/apps", bytes.NewBuffer(body))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	return nil
}

func (c *Client) DeleteApp(uuid string) error {
	body, err := json.Marshal(map[string]string{"uuid": uuid})
	if err != nil {
		return err
	}

	req, err := http.NewRequest("POST", c.BaseURL+"/api/apps/delete", bytes.NewBuffer(body))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status %d: %s", resp.StatusCode, string(raw))
	}

	return nil
}

// UpdateApp updates an existing app. Apollo matches by UUID in the JSON body.
func (c *Client) UpdateApp(app App) error {
	body, err := json.Marshal(app)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("POST", c.BaseURL+"/api/apps", bytes.NewBuffer(body))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	return nil
}

