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
	"archive/zip"
	"bytes"
	"encoding/json"
	"encoding/xml"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mattn/go-xmpp"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"gopkg.in/yaml.v3"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type Fdroid struct {
	Repo struct {
		Name string
	}
	Apps     []Application
	Packages map[string][]Package
}

func (fdroid *Fdroid) AppIDList() []string {
	var appIDs []string
	for _, app := range fdroid.Apps {
		appIDs = append(appIDs, app.PackageName)
	}
	return appIDs
}

type Application struct {
	PackageName string
	Name        string
	Localized   struct {
		Default struct {
			Name string
		} `json:"en-US"`
	}
}

func (app *Application) GetName() string {
	if app.Localized.Default.Name != "" {
		return app.Localized.Default.Name
	}
	return app.Name
}

type Package struct {
	VersionCode int
	VersionName string
}

type PingRequest struct {
	XMLName xml.Name `xml:"urn:xmpp:ping ping"`
}

type IQErrorNotAcceptable struct {
	XMLName xml.Name `xml:"error"`
	Type    string   `xml:"type,attr"`
	Error   struct {
		XMLName xml.Name `xml:"urn:ietf:params:xml:ns:xmpp-stanzas not-acceptable"`
	}
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

const aboutMsg = `Hi I'm a bot reporting about updates in F-Droid repos.

I was made by j.r. My code is licensed under AGPL-3.0-or-later and could be found at https://git.sr.ht/~j-r/fdroid-news`

func main() {
	var configFile, passwordFile string
	var debugMode bool
	flag.StringVar(&configFile, "c", "", "Config file")
	flag.StringVar(&passwordFile, "p", "", "Optionally pass a file that only contains the password for the XMPP user")
	flag.BoolVar(&debugMode, "v", false, "Print intensive log information")
	flag.Parse()

	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stdout, NoColor: true, TimeFormat: time.RFC3339})
	zerolog.SetGlobalLevel(zerolog.InfoLevel)
	if debugMode {
		zerolog.SetGlobalLevel(zerolog.TraceLevel)
	}

	db, err := gorm.Open(sqlite.Open("fdroid-news.sqlite"), &gorm.Config{
		PrepareStmt: true,
	})
	if err != nil {
		log.Fatal().Stack().Err(err).Msg("Erorr opening database")
	}

	if err := db.AutoMigrate(&DBApplication{}); err != nil {
		log.Fatal().Stack().Err(err).Msg("Error migrating db")
	}

	if configFile == "" {
		log.Fatal().Msg("Please provide a config file using the -c flag")
	}

	configFileContent, err := os.ReadFile(configFile)
	if err != nil {
		log.Fatal().Stack().Err(err).Msg("Error reading config file")
	}

	var config Config
	err = yaml.Unmarshal(configFileContent, &config)
	if err != nil {
		log.Fatal().Stack().Err(err).Msg("Error parsing YAML")
	}

	if config.XMPP.Password == "" && passwordFile != "" {
		password, err := os.ReadFile(passwordFile)
		if err != nil {
			log.Fatal().Stack().Err(err).Msg("Cannot open password file")
		}
		config.XMPP.Password = string(password)
	} else if config.XMPP.Password == "" && passwordFile == "" {
		log.Fatal().Msg("Please provide XMPP password either via config or password file")
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
		log.Fatal().Stack().Err(err).Msg("Can't create XMPP client from options")
	}

	go processIncommingStanzas(client, config)

	_, err = client.JoinMUCNoHistory(config.XMPP.MUC, config.XMPP.Nick)
	if err != nil {
		log.Fatal().Stack().Err(err).Msg("Unable to join MUC")
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
	log.Info().Msg("First time initialising database from index")

	fdroid, err := getIndex(repo)
	if err != nil {
		log.Fatal().Stack().Err(err).Msg("Could not init DB")
	}
	appMap := make(map[string]Application)
	for _, app := range fdroid.Apps {
		appMap[app.PackageName] = app
	}
	saveNewApps(appMap, db, repo, fdroid.Packages)
}

func checkUpdates(wg *sync.WaitGroup, db *gorm.DB, client *xmpp.Client, config *Config, repo string) {
	log.Debug().Msg("Starting update check")

	fdroid, err := getIndex(repo)
	if err != nil {
		log.Warn().Err(err).Msg("")
		return
	}

	var knownApps []DBApplication

	appIdList := fdroid.AppIDList()

	db.Where(&DBApplication{Repo: repo}).Where("app_id IN ? ", appIdList).Find(&knownApps)

	repoApps := make(map[string]Application)
	for _, app := range fdroid.Apps {
		repoApps[app.PackageName] = app
	}

	log.Debug().Msg("Finished fetching apps")

	var updatedApps []DBApplication
	for _, app := range knownApps {
		packages := fdroid.Packages[app.AppId]
		repoApp := repoApps[app.AppId]
		updated := false
		for _, pack := range packages {
			if app.VersionCode < pack.VersionCode {
				app.Name = repoApp.GetName()
				app.Version = pack.VersionName
				app.VersionCode = pack.VersionCode
				updated = true
			}
		}
		if updated {
			updatedApps = append(updatedApps, app)
		}
		delete(repoApps, app.AppId)
	}

	if len(updatedApps) > 0 {
		db.Save(&updatedApps)
	}

	log.Debug().Msg("Found all updated apps")

	var addedApps []*DBApplication
	if len(repoApps) > 0 {
		addedApps = saveNewApps(repoApps, db, repo, fdroid.Packages)
	}

	log.Debug().Msg("Found all new apps")

	if len(addedApps) == 0 && len(updatedApps) == 0 {
		log.Debug().Msg("No new apps")
		return
	}

	log.Debug().Msg("Constructing output...")

	var builder strings.Builder

	sort.Slice(addedApps, func(i, j int) bool {
		return strings.ToUpper(addedApps[i].Name) < strings.ToUpper(addedApps[j].Name)
	})
	sort.Slice(updatedApps, func(i, j int) bool {
		return strings.ToUpper(updatedApps[i].Name) < strings.ToUpper(updatedApps[j].Name)
	})

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

	log.Debug().Msg(builder.String())

	_, err = client.Send(xmpp.Chat{
		Remote: config.XMPP.MUC,
		Type:   "groupchat",
		Text:   builder.String(),
	})
	if err != nil {
		log.Fatal().Stack().Err(err).Msg("Error sending groupchat message")
	}

	wg.Done()
}

func getIndex(repo string) (Fdroid, error) {
	repoURL, err := url.Parse(repo)
	if err != nil {
		log.Fatal().Stack().Err(err).Msg("Can't parse repo URL")
	}
	repoURL.RawQuery = ""
	repoURL.Path += "/index-v1.jar"
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

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Fatal().Stack().Err(err).Msg("Could not read response")
	}

	r, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		log.Fatal().Stack().Err(err).Msg("Error while parsing as zip")
	}

	index, err := r.Open("index-v1.json")
	if err != nil {
		log.Fatal().Stack().Err(err).Msg("Error opening index file from zip")
	}

	var fdroid Fdroid
	if err = json.NewDecoder(index).Decode(&fdroid); err != nil {
		log.Fatal().Stack().Err(err).Msg("Error deconding json")
	}

	return fdroid, nil
}

func saveNewApps(newApps map[string]Application, db *gorm.DB, repo string, packages map[string][]Package) (addedApps []*DBApplication) {
	for key, app := range newApps {
		log.Printf("Processing %s for DB save", key)
		var latestPackage Package
		for _, pack := range packages[app.PackageName] {
			if latestPackage.VersionCode < pack.VersionCode {
				latestPackage = pack
			}
		}
		dbApp := DBApplication{
			AppId:       app.PackageName,
			Name:        app.GetName(),
			Version:     latestPackage.VersionName,
			VersionCode: latestPackage.VersionCode,
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
		log.Warn().Stack().Err(err).Msg("C2S ping failed")
	}

	err = client.PingC2S("", config.XMPP.MUC+"/"+config.XMPP.Nick)
	if err != nil {
		log.Warn().Stack().Err(err).Msg("MUC ping failed")
	}
	wg.Done()
}

func processIncommingStanzas(client *xmpp.Client, config Config) {
	for {
		stanza, err := client.Recv()
		if err != nil {
			log.Error().Stack().Err(err).Msg("Can't receive stanzas")
			return
		}

		switch value := stanza.(type) {
		case xmpp.IQ:
			if value.Type == "get" {
				err := xml.Unmarshal(value.Query, &PingRequest{})
				if err == nil {
					log.Debug().Msg("Sending ping response")
					if err := client.SendResultPing(value.ID, value.From); err != nil {
						log.Error().Stack().Err(err).Msg("Error during ping response")
						continue
					}
				} else if err := xml.Unmarshal(value.Query, &IQErrorNotAcceptable{}); err == nil {
					if value.From == fmt.Sprintf("%s/%s", config.XMPP.MUC, config.XMPP.Nick) {
						client.JoinMUCNoHistory(config.XMPP.MUC, config.XMPP.Nick)
						log.Debug().Msg("Rejoined MUC because ping failed")
					}
				} else {
					iqError := IQErrorServiceUnavailable{
						Type:  "cancel",
						Error: ErrorServiceUnavailable{},
					}

					response, err := xml.Marshal(iqError)
					if err != nil {
						log.Error().Stack().Err(err).Msg("Error marshalling service unavailable error")
						continue
					}
					if log.Debug().Enabled() {
						log.Debug().Str("response", string(response)).Msg("Sending the error response")
					}

					_, err = client.RawInformation(value.To, value.From, value.ID, "error", string(response))
					if err != nil {
						log.Error().Stack().Err(err).Msg("Sending service unavailable failed")
						continue
					}
				}
			}
		case xmpp.Chat:
			if value.Type == "groupchat" {
				remoteSplit := strings.Split(value.Remote, "/")
				if len(remoteSplit) <= 1 {
					break
				}
				if remoteSplit[1] != config.XMPP.Nick {
					r, _ := regexp.Compile(fmt.Sprintf(`^%s[\s,:]`, regexp.QuoteMeta(config.XMPP.Host)))
					if r.Match([]byte(value.Text)) {
						_, err := client.Send(xmpp.Chat{
							Remote: strings.Split(value.Remote, "/")[0],
							Type:   "groupchat",
							Text:   aboutMsg,
						})
						if err != nil {
							log.Printf("Error sending aboutMsg: %v", err)
						}
					}
				}
			}
		default:
			break
		}
	}
}
