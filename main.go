/*
fdroid-news, a XMPP bot for posting news about F-Droid repos.
Copyright (C) 2021  j.r

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program.  If not, see <https://www.gnu.org/licenses/>.
*/

package main

import (
	"encoding/xml"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/mattn/go-xmpp"
	"gopkg.in/yaml.v3"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type Fdroid struct {
	Repo struct {
		Name string `xml:"name,attr"`
	} `xml:"repo"`
	Applications []Application `xml:"application"`
}

func (fdroid *Fdroid) AppIDList() []string {
	var appIDs []string
	for _, app := range fdroid.Applications {
		appIDs = append(appIDs, app.ID)
	}
	return appIDs
}

type Application struct {
	ID          string `xml:"id"`
	Name        string `xml:"name"`
	Version     string `xml:"marketversion"`
	VersionCode int    `xml:"marketvercode"`
}

type PingRequest struct {
	XMLName xml.Name `xml:"urn:xmpp:ping ping"`
}

type IQErrorServiceUnavailable struct {
	XMLName xml.Name `xml:"error"`
	Type    string   `xml:"type,attr"`
	Error   ErrorServiceUnavailable
}

type ErrorServiceUnavailable struct {
	XMLName xml.Name `xml:"urn:ietf:params:xml:ns:xmpp-stanzas service-unavailable"`
}

type DBApplication struct {
	gorm.Model
	AppId       string `gorm:"index"`
	Name        string
	Version     string
	VersionCode int
	Repo        string `gorm:"index"`
}

type Config struct {
	XMPP struct {
		Username string
		Host     string
		Password string
		MUC      string
		Nick     string
	}
	Repos []string `yaml:",flow"`
}

func main() {
	db, err := gorm.Open(sqlite.Open("fdroid-news.sqlite"), &gorm.Config{
		PrepareStmt: true,
	})
	if err != nil {
		log.Fatal(err.Error())
	}

	db.AutoMigrate(&DBApplication{})

	var configFile string
	flag.StringVar(&configFile, "c", "", "Config file")
	flag.Parse()

	if configFile == "" {
		log.Fatal("Please provide a config file using the -c flag")
	}

	configFileContent, err := ioutil.ReadFile(configFile)
	if err != nil {
		log.Fatal(err.Error())
	}

	var config Config
	err = yaml.Unmarshal(configFileContent, &config)
	if err != nil {
		log.Fatalf("Error parsing YAML: %s", err.Error())
	}

	for _, repo := range config.Repos {
		var count int64
		db.Model(&DBApplication{}).Where(&DBApplication{Repo: repo}).Count(&count)
		if count == 0 {
			initDB(db, repo)
		}
	}

	options := xmpp.Options{
		Host:     config.XMPP.Host,
		User:     config.XMPP.Username,
		Password: config.XMPP.Password,
	}
	client, err := options.NewClient()
	if err != nil {
		log.Fatal(err.Error())
	}

	go processIncommingStanzas(client)

	_, err = client.JoinMUCNoHistory(config.XMPP.MUC, config.XMPP.Nick)
	if err != nil {
		log.Fatal(err.Error())
	}

	var wg sync.WaitGroup

	pingTicker := time.NewTicker(30 * time.Second)

	wg.Add(1)
	go func() {
		for range pingTicker.C {
			wg.Add(1)
			doPings(client, &wg, &config)
		}
		wg.Done()
	}()

	ticker := time.NewTicker(15 * time.Minute)

	for _, repo := range config.Repos {
		wg.Add(1)
		go checkUpdates(&wg, db, client, &config, repo)
	}
	wg.Add(1)
	go func() {
		for range ticker.C {
			for _, repo := range config.Repos {
				wg.Add(1)
				go checkUpdates(&wg, db, client, &config, repo)
			}
		}
		wg.Done()
	}()

	wg.Wait()
}

func initDB(db *gorm.DB, repo string) {
	log.Print("Init DB from index...")

	fdroid, err := getIndex(repo)
	if err != nil {
		if err.(net.Error).Temporary() {
			log.Fatal("Could not init DB because of timeout, please restart later")
		}
	}
	appMap := make(map[string]Application)
	for _, app := range fdroid.Applications {
		appMap[app.ID] = app
	}
	saveNewApps(appMap, db, repo)
}

func checkUpdates(wg *sync.WaitGroup, db *gorm.DB, client *xmpp.Client, config *Config, repo string) {
	log.Print("Starting update check...")

	fdroid, err := getIndex(repo)
	if err != nil {
		if err, ok := err.(net.Error); ok && err.Temporary() {
			return
		} else {
			log.Print(err.Error())
		}
	}

	var knownApps []DBApplication

	appIdList := fdroid.AppIDList()

	db.Where(&DBApplication{Repo: repo}).Where("app_id IN ? ", appIdList).Find(&knownApps)

	repoApps := make(map[string]Application)
	var updatedApps []DBApplication
	for _, app := range fdroid.Applications {
		repoApps[app.ID] = app
	}

	log.Print("Finished fetching apps")

	for _, app := range knownApps {
		repoApp := repoApps[app.AppId]
		if app.VersionCode < repoApp.VersionCode {
			app.Name = repoApp.Name
			app.Version = repoApp.Version
			app.VersionCode = repoApp.VersionCode
			updatedApps = append(updatedApps, app)
		}
		delete(repoApps, app.AppId)
	}
	if len(updatedApps) > 0 {
		db.Save(&updatedApps)
	}

	log.Print("Found all updated apps")

	var addedApps []*DBApplication
	if len(repoApps) > 0 {
		addedApps = saveNewApps(repoApps, db, repo)
	}

	log.Print("Found all new apps")

	if len(addedApps) == 0 && len(updatedApps) == 0 {
		log.Print("No new apps")
		return
	}

	log.Print("Contructing output...")

	var builder strings.Builder

	builder.WriteString(fmt.Sprintf("*⟳ %d apps added, %d updated at %s*\n\n", len(addedApps), len(updatedApps), fdroid.Repo.Name))
	if len(addedApps) > 0 {
		builder.WriteString(fmt.Sprintf("*Added (%d)*\n", len(addedApps)))
		for _, app := range addedApps {
			builder.WriteString(fmt.Sprintf("* %s\n", app.Name))
		}
	}
	if len(updatedApps) > 0 {
		builder.WriteString(fmt.Sprintf("*Updated (%d)*\n", len(updatedApps)))
		for _, app := range updatedApps {
			builder.WriteString(fmt.Sprintf("* %s\n", app.Name))
		}
	}

	log.Print(builder.String())

	_, err = client.Send(xmpp.Chat{
		Remote: config.XMPP.MUC,
		Type:   "groupchat",
		Text:   builder.String(),
	})
	if err != nil {
		log.Fatal(err.Error())
	}

	wg.Done()
}

func getIndex(repo string) (Fdroid, error) {
	repoURL, err := url.Parse(repo)
	if err != nil {
		log.Fatal(err.Error())
	}
	repoURL.RawQuery = ""
	repoURL.Path += "/index.xml"
	log.Printf("Getting %s", repoURL.String())
	resp, err := http.Get(repoURL.String())
	if err != nil {
		if err, ok := err.(net.Error); ok && err.Temporary() {
			log.Printf("Temporary error reaching %s", repoURL.String())
			return Fdroid{}, err
		} else {
			return Fdroid{}, err
		}
	}
	var fdroid Fdroid

	b, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Fatal(err.Error())
	}

	xml.Unmarshal(b, &fdroid)

	return fdroid, nil
}

func saveNewApps(newApps map[string]Application, db *gorm.DB, repo string) (addedApps []*DBApplication) {
	for key, app := range newApps {
		log.Printf("Processing %s for DB save", key)
		dbApp := DBApplication{
			AppId:       app.ID,
			Name:        app.Name,
			Version:     app.Version,
			VersionCode: app.VersionCode,
			Repo:        repo,
		}
		addedApps = append(addedApps, &dbApp)
	}
	log.Printf("Saving %d apps to DB", len(addedApps))
	if len(addedApps) > 0 {
		db.Create(&addedApps)
	}
	return
}

func doPings(client *xmpp.Client, wg *sync.WaitGroup, config *Config) {
	err := client.PingC2S("", "")
	if err != nil {
		log.Fatalf("C2S ping failed with: %s", err.Error())
	}

	err = client.PingC2S("", config.XMPP.MUC+"/"+config.XMPP.Nick)
	if err != nil {
		log.Fatalf("MUC ping failed with %s", err.Error())
	}
	wg.Done()
}

func processIncommingStanzas(client *xmpp.Client) {
	for {
		stanza, err := client.Recv()
		if err != nil {
			log.Fatal(err.Error())
		}

		switch value := stanza.(type) {
		case xmpp.IQ:
			log.Printf("Incomming iq, type: %s, query: %s, id: %s, from: %s", value.Type, string(value.Query), value.ID, value.From)
			if value.Type == "get" {
				err := xml.Unmarshal(value.Query, &PingRequest{})
				if err == nil {
					log.Print("Sending ping response")
					client.SendResultPing(value.ID, value.From)
				} else {
					iqError := IQErrorServiceUnavailable{
						Type:  "cancel",
						Error: ErrorServiceUnavailable{},
					}

					response, err := xml.Marshal(iqError)
					if err != nil {
						log.Fatal(err.Error())
					}

					log.Printf("Sending error response: %s", string(response))

					_, err = client.RawInformation(value.To, value.From, value.ID, "error", string(response))
					if err != nil {
						log.Fatal(err.Error())
					}
				}
			}
		}
	}
}
