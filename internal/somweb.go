package internal

import (
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

type any = interface{}

var webtokenRe = regexp.MustCompile(`(?m)<\s*input\s+id\s*=\s*"webtoken".+value\s*=\s*"(?P<webtoken>\w+)"`)

type Client struct {
	Host     string
	Username string
	Password string

	client   *http.Client
	cookies  []*http.Cookie
	webtoken string
}

func NewClient(host, username, password string) (*Client, error) {
	c := &Client{
		Host:     host,
		Username: username,
		Password: password,

		client: &http.Client{},
	}
	return c, c.authenticate()
}

func (c *Client) addCookies(req *http.Request) {
	for _, cookie := range c.cookies {
		req.AddCookie(cookie)
	}
}

func (c *Client) CloseDoor(door string) error {
	log.Println("CloseDoor", door)
	doors, err := c.Doors()
	if err != nil {
		return fmt.Errorf("unable to get all doors: %w", err)
	}
	if !doors[door] {
		return nil
	}
	return c.changeDoorState(door, "1")
}

func (c *Client) OpenDoor(door string) error {
	log.Println("OpenDoor", door)
	doors, err := c.Doors()
	if err != nil {
		return fmt.Errorf("unable to get all doors: %w", err)
	}
	if doors[door] {
		return nil
	}
	return c.changeDoorState(door, "0")
}

func (c *Client) ToggleDoor(door string) error {
	log.Println("ToggleDoor", door)
	doors, err := c.Doors()
	if err != nil {
		return fmt.Errorf("unable to get all doors: %w", err)
	}
	var action string
	if doors[door] {
		action = "1"
	}
	return c.changeDoorState(door, action)
}

func (c *Client) Doors() (map[string]bool, error) {
	req, err := http.NewRequest("GET", c.path("/isg/statusDoorAll.php?access=1&login="+c.Username+"&webtoken="+c.webtoken), nil)
	if err != nil {
		return nil, fmt.Errorf("unable to get all doors: %w", err)
	}
	c.addCookies(req)
	var resp *http.Response
	if resp, err = c.do(req); err != nil {
		return nil, fmt.Errorf("unable to get all doors: %w", err)
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)
	var b []byte
	if b, err = ioutil.ReadAll(resp.Body); err != nil {
		return nil, fmt.Errorf("unable to read response body: %w", err)
	}
	var doors struct {
		Num1  any    `json:"1"`
		Num2  any    `json:"2"`
		Num3  any    `json:"3"`
		Num4  any    `json:"4"`
		Num5  any    `json:"5"`
		Num6  any    `json:"6"`
		Num7  any    `json:"7"`
		Num8  any    `json:"8"`
		Num9  any    `json:"9"`
		Num10 any    `json:"10"`
		Num11 string `json:"11"`
	}
	if err = json.Unmarshal(b, &doors); err != nil {
		return nil, fmt.Errorf("unable to unmarshal doors response: %w", err)
	}
	c.webtoken = doors.Num11
	s := map[string]bool{
		"1":  determineDoorState(doors.Num1),
		"2":  determineDoorState(doors.Num2),
		"3":  determineDoorState(doors.Num3),
		"4":  determineDoorState(doors.Num4),
		"5":  determineDoorState(doors.Num5),
		"6":  determineDoorState(doors.Num6),
		"7":  determineDoorState(doors.Num7),
		"8":  determineDoorState(doors.Num8),
		"9":  determineDoorState(doors.Num9),
		"10": determineDoorState(doors.Num10),
	}
	log.Println("doors", s)
	return s, nil
}

func (c *Client) authenticate() (err error) {
	form := url.Values{
		"login":      {c.Username},
		"pass":       {c.Password},
		"send-login": {"Sign in"},
	}

	var req *http.Request
	if req, err = http.NewRequest("POST", c.path("/index.php"), strings.NewReader(form.Encode())); err != nil {
		return fmt.Errorf("unable to create request: %w", err)
	}
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")

	var resp *http.Response
	if resp, err = c.do(req); err != nil {
		return fmt.Errorf("unable to send request: %w", err)
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)
	c.cookies = resp.Cookies()
	var b []byte
	if b, err = ioutil.ReadAll(resp.Body); err != nil {
		return fmt.Errorf("unable to read response body: %w", err)
	}
	if err = c.extractWebtoken(b); err != nil {
		return fmt.Errorf("unable to extract webtoken: %w", err)
	}
	return nil
}

func (c *Client) changeDoorState(door, status string) error {
	log.Println("changeDoorState", door)
	req, err := http.NewRequest("GET", c.path(fmt.Sprintf("/isg/opendoor.php?numdoor=%s&status=%s&webtoken=%s", door, status, c.webtoken)), nil)
	if err != nil {
		return fmt.Errorf("unable to create request: %w", err)
	}
	c.addCookies(req)
	var resp *http.Response
	if resp, err = c.do(req); err != nil {
		return fmt.Errorf("unable to change door state: %w", err)
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)
	var b []byte
	if b, err = ioutil.ReadAll(resp.Body); err != nil {
		return fmt.Errorf("unable to read response body: %w", err)
	}
	log.Println("changeDoorState", string(b))
	return nil
}

func (c *Client) do(req *http.Request) (*http.Response, error) {
	return c.client.Do(req)
}

func (c *Client) extractWebtoken(b []byte) error {
	submatch := webtokenRe.FindSubmatch(b)
	for i, name := range webtokenRe.SubexpNames() {
		if name == "webtoken" {
			c.webtoken = string(submatch[i])
			return nil
		}
	}
	return fmt.Errorf("unable to find webtoken in response body")
}

func (c *Client) path(uri string) string {
	return "http://" + c.Host + uri
}

func determineDoorState(door any) bool {
	if door != nil {
		if f, ok := door.(float64); ok {
			if f == 1 {
				return true
			} else {
				return false
			}
		}
		if s, ok := door.(string); ok {
			if s == "1" {
				return true
			} else {
				return false
			}
		}
	}
	return false
}
