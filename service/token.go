package service

import (
	"crypto/md5"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/alexzorin/authy"
	"github.com/zalando/go-keyring"
	"golang.org/x/crypto/ssh/terminal"
)

// Tokens for sort
type Tokens []*Token

func (t Tokens) Less(i, j int) bool {
	return t[i].Weight > t[j].Weight
}

func (t Tokens) Len() int {
	return len(t)
}

func (t Tokens) Swap(i, j int) {
	t[i], t[j] = t[j], t[i]
}

// String implement fuzz search
func (t Tokens) String(i int) string {
	return t[i].Name + t[i].OriginalName
}

// Token ..
type Token struct {
	Name         string `json:"name"`
	OriginalName string `json:"original_name"`
	Digital      int    `json:"digital"`
	Secret       string `json:"secret"`
	Period       int    `json:"period"`
	Weight       int    `json:"weight"`
}

// Title show string
func (t Token) Title() string {
	if len(t.Name) > len(t.OriginalName) {
		return t.Name
	}

	return t.OriginalName
}

func (t *Token) updateWeight() {
	t.Weight++
}

// LoadTokenFromCache load token from local cache
func (d *Device) LoadTokenFromCache() (err error) {
	defer func() {
		if err != nil {
			d.LoadTokenFromAuthyServer()
			err = nil
		}
	}()

	res, err := keyring.Get("authy", d.conf.Mobile)
	if err != nil {
		return
	}

	err = json.Unmarshal([]byte(res), &d.tokens)

	d.tokenMap = tokensToMap(d.tokens)

	return
}

// LoadTokenFromAuthyServer load token from authy server, make sure that you've enabled Authenticator Backups And Multi-Device Sync
func (d *Device) LoadTokenFromAuthyServer() {
	client, err := authy.NewClient()
	if err != nil {
		log.Fatalf("Create authy API client failed %+v", err)
	}

	apps, err := client.QueryAuthenticatorApps(nil, d.registration.UserID, d.registration.DeviceID, d.registration.Seed)
	if err != nil {
		log.Fatalf("Fetch authenticator apps failed %+v", err)
	}

	if !apps.Success {
		log.Fatalf("Fetch authenticator apps failed %+v", apps)
	}

	tokens, err := client.QueryAuthenticatorTokens(nil, d.registration.UserID, d.registration.DeviceID, d.registration.Seed)
	if err != nil {
		log.Fatalf("Fetch authenticator tokens failed %+v", err)
	}

	if !tokens.Success {
		log.Fatalf("Fetch authenticator tokens failed %+v", tokens)
	}

	mainpwd := d.getMainPassword()

	tks := []*Token{}
	for _, v := range tokens.AuthenticatorTokens {
		secret, err := v.Decrypt(mainpwd)
		if err != nil {
			log.Fatalf("Decrypt token failed %+v", err)
		}

		tks = append(tks, &Token{
			Name:         v.Name,
			OriginalName: v.OriginalName,
			Digital:      v.Digits,
			Secret:       secret,
		})
	}

	for _, v := range apps.AuthenticatorApps {
		secret, err := v.Token()
		if err != nil {
			log.Fatal("Get secret from app failed", err)
		}

		tks = append(tks, &Token{
			Name:    v.Name,
			Digital: v.Digits,
			Secret:  secret,
			Period:  10,
		})
	}

	d.tokenMap = tokensToMap(tks)
	d.tokens = tks
	return
}

func (d *Device) getMainPassword() string {
	if len(d.registration.MainPassword) == 0 {
		fmt.Print("\nPlease input Authy main password: ")
		pp, err := terminal.ReadPassword(int(os.Stdin.Fd()))
		if err != nil {
			log.Fatalf("Get password failed %+v", err)
		}

		d.registration.MainPassword = strings.TrimSpace(string(pp))
		d.SaveDeviceInfo()
	}

	return d.registration.MainPassword
}

func generateMD5(tk *Token) string {
	return fmt.Sprintf("%x", md5.Sum([]byte(tk.Name+tk.OriginalName+tk.Secret)))
}

func tokensToMap(tks []*Token) map[string]*Token {
	ret := map[string]*Token{}
	for _, tk := range tks {
		ret[generateMD5(tk)] = tk
	}

	return ret
}

func (d *Device) saveToken() {
	tokens := make([]Token, 0, len(d.tokenMap))
	for _, v := range d.tokenMap {
		tokens = append(tokens, *v)
	}

	res, err := json.Marshal(tokens)
	if err != nil {
		return
	}

	err = keyring.Set("authy", d.conf.Mobile, string(res))
	if err != nil {
		return
	}
	return
}
