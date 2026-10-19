package main

import (
	"fmt"
	"log"
	"os"
	"reflect"
)

// regionDef is everything that differs between the ru and com deployments
// apart from env config: the language of the login page and which login
// methods the zone offers. The values live in region-ru.go and region-com.go.
// Technical text (logs, JSON error bodies) is English in both and stays out.
type regionDef struct {
	Lang string // <html lang>

	// Telegram login is offered. Off: the tile only explains, in a popup, that
	// the method is unavailable, and POST /qr-session refuses with 403.
	Telegram bool

	Text pageText
}

// pageText holds the strings of web/index.html. Fields, not a map: a typo in
// the template fails at render instead of silently printing nothing.
type pageText struct {
	Title      string // <title>
	SignInWith string // heading above the tiles

	TileGoogle   string
	TileTelegram string
	TileSolana   string

	OpenInTelegram string
	Refresh        string // after the Telegram session expired

	ConnectWallet string
	NoWallet      string // before the wallet links: "<NoWallet> phantom / solflare"
	OpenInPhantom string

	// popup on a login method the region does not offer
	Unavailable string
	OK          string

	JS jsText // strings set by script.js
}

// jsText is rendered into the page as JSON and read by script.js.
type jsText struct {
	ConnectWallet    string `json:"connectWallet"`
	Connecting       string `json:"connecting"`
	LookingForWallet string `json:"lookingForWallet"`
	OpeningWallet    string `json:"openingWallet"`
	Signing          string `json:"signing"`
	Connected        string `json:"connected"`
}

var regions = map[string]*regionDef{
	"ru":  &regionRU,
	"com": &regionCOM,
}

// Active region, set once by initRegion before anything serves.
var (
	regionName string
	region     *regionDef
)

// initRegion picks the region by REGION (ru | com, set by the deploy matrix).
// Empty means local dev and falls back to ru; anything else is a config error
// and stops the process rather than serving the wrong zone — the health check
// then fails and deploy.sh rolls back. A string forgotten in either region is
// fatal too, whichever region this instance serves.
func initRegion() {
	for name, r := range regions {
		if err := checkFilled(reflect.ValueOf(*r), "region"); err != nil {
			log.Fatalf("region %s: %v", name, err)
		}
	}

	regionName = os.Getenv("REGION")
	if regionName == "" {
		regionName = "ru"
	}
	r, ok := regions[regionName]
	if !ok {
		log.Fatalf("unknown REGION %q (want ru or com)", regionName)
	}
	region = r
	log.Printf("region: %s (lang=%s telegram=%t)", regionName, region.Lang, region.Telegram)
}

// checkFilled walks structs and fails on the first empty string.
func checkFilled(v reflect.Value, path string) error {
	switch v.Kind() {
	case reflect.String:
		if v.Len() == 0 {
			return fmt.Errorf("%s is empty", path)
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if err := checkFilled(v.Field(i), path+"."+v.Type().Field(i).Name); err != nil {
				return err
			}
		}
	}
	return nil
}
