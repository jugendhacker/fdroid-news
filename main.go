package main

import (
	"encoding/xml"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/mattn/go-xmpp"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type Fdroid struct {
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

type DBApplication struct {
	gorm.Model
	AppId       string `gorm:"index"`
	Name        string
	Version     string
	VersionCode int
}

func main() {
	db, err := gorm.Open(sqlite.Open("fdroid-news.sqlite"), &gorm.Config{
		PrepareStmt: true,
	})
	if err != nil {
		log.Fatal(err.Error())
	}

	db.AutoMigrate(&DBApplication{})

	var count int64
	db.Model(&DBApplication{}).Count(&count)
	if count == 0 {
		initDB(db)
	}

	options := xmpp.Options{
		Host:     "example.org:5223",
		User:     "feedbot@example.org",
		Password: "examplePassword",
	}
	client, err := options.NewClient()
	if err != nil {
		log.Fatal(err.Error())
	}

	_, err = client.JoinMUCNoHistory("newsbot-pg@conference.jugendhacker.de", "News")
	if err != nil {
		log.Fatal(err.Error())
	}

	var wg sync.WaitGroup

	pingTicker := time.NewTicker(30 * time.Second)

	wg.Add(1)
	go func() {
		for range pingTicker.C {
			wg.Add(1)
			doPings(client, &wg)
		}
		wg.Done()
	}()

	ticker := time.NewTicker(15 * time.Minute)

	wg.Add(1)
	go checkUpdates(&wg, db, client)
	wg.Add(1)
	go func() {
		for range ticker.C {
			wg.Add(1)
			go checkUpdates(&wg, db, client)
		}
		wg.Done()
	}()

	wg.Wait()
}

func initDB(db *gorm.DB) {
	log.Print("Init DB from index...")

	fdroid, err := getIndex()
	if err.(net.Error).Temporary() {
		log.Fatal("Could not init DB because of timeout, please restart later")
	}
	appMap := make(map[string]Application)
	for _, app := range fdroid.Applications {
		appMap[app.ID] = app
	}
	saveNewApps(appMap, db)
}

func checkUpdates(wg *sync.WaitGroup, db *gorm.DB, client *xmpp.Client) {
	log.Print("Starting update check...")

	fdroid, err := getIndex()
	if err != nil {
		if err, ok := err.(net.Error); ok && err.Temporary() {
			return
		} else {
			log.Print(err.Error())
		}
	}

	var knownApps []DBApplication

	appIdList := fdroid.AppIDList()

	db.Where("app_id IN ? ", appIdList).Find(&knownApps)

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
		addedApps = saveNewApps(repoApps, db)
	}

	log.Print("Found all new apps")

	if len(addedApps) == 0 && len(updatedApps) == 0 {
		log.Print("No new apps")
		return
	}

	log.Print("Contructing output...")

	var builder strings.Builder

	builder.WriteString(fmt.Sprintf("*⟳ %d apps added, %d updated at f-droid.org*\n\n", len(addedApps), len(updatedApps)))
	if len(addedApps) > 0 {
		builder.WriteString(fmt.Sprintf("*Added (%d)*\n", len(addedApps)))
		for _, app := range addedApps {
			builder.WriteString(fmt.Sprintf("* %s\n", app.Name))
		}
	}
	if len(updatedApps) > 0 {
		builder.WriteString(fmt.Sprintf("*Updated (%d)*\n", len(addedApps)))
		for _, app := range updatedApps {
			builder.WriteString(fmt.Sprintf("* %s\n", app.Name))
		}
	}

	log.Print(builder.String())

	_, err = client.Send(xmpp.Chat{
		Remote: "newsbot-pg@conference.jugendhacker.de",
		Type:   "groupchat",
		Text:   builder.String(),
	})
	if err != nil {
		log.Fatal(err.Error())
	}

	wg.Done()
}

func getIndex() (Fdroid, error) {
	resp, err := http.Get("https://f-droid.org/repo/index.xml")
	if err != nil {
		if err, ok := err.(net.Error); ok && err.Temporary() {
			log.Printf("Temporary error reaching %s", "https://f-droid.org/repo/index.xml")
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

func saveNewApps(newApps map[string]Application, db *gorm.DB) (addedApps []*DBApplication) {
	for key, app := range newApps {
		log.Printf("Processing %s for DB save", key)
		dbApp := DBApplication{
			AppId:       app.ID,
			Name:        app.Name,
			Version:     app.Version,
			VersionCode: app.VersionCode,
		}
		addedApps = append(addedApps, &dbApp)
	}
	log.Printf("Saving %d apps to DB", len(addedApps))
	if len(addedApps) > 0 {
		db.Create(&addedApps)
	}
	return
}

func doPings(client *xmpp.Client, wg *sync.WaitGroup) {
	err := client.PingC2S("", "")
	if err != nil {
		log.Fatalf("C2S ping failed with: %e", err)
	}

	err = client.PingC2S("", "newsbot-pg@conference.jugendhacker.de/News")
	if err != nil {
		log.Fatalf("MUC ping failed with %e", err)
	}
	wg.Done()
}
