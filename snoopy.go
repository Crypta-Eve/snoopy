package main

import (
	"database/sql"
	"embed"
	"encoding/base64"
	"html/template"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/middleware"

	"crypto/sha512"

	"github.com/go-chi/chi"
	_ "github.com/go-sql-driver/mysql"
	"github.com/gobuffalo/envy"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

type Record struct {
	ID        uint64    `gorm:"primarykey"`
	CreatedAt time.Time `gorm:"index"`
	UpdatedAt time.Time `gorm:"index"`
	DeletedAt gorm.DeletedAt

	IP   string `gorm:"index;size:512"`
	Slug string
}

type Hit struct {
	IP   string
	Slug string
}

type StatData struct {
	FirstRecordDate time.Time
	Totals          []StatList
	Sessions        []StatList
	SessionTime     int
	PluginUsers     map[string][]StatList
	PluginSessions  map[string][]StatList
}

type StatList struct {
	Users uint32
	Time  int
}

var (
	//go:embed assets/*
	assetData embed.FS
)

func main() {

	// Load env variables in order to connect to redis and mariadb
	envy.Load()

	// Maria connection!
	mariaHost := envy.Get("MARIA_HOST", "localhost")
	mariaUser := envy.Get("MARIA_USER", "snoopy")
	mariaPass := envy.Get("MARIA_PASS", "snoopy")
	mariaDatabase := envy.Get("MARIA_DATABASE", "snoopy")

	statSlugs := strings.Split(envy.Get("STAT_SLUGS", "%"), ",")
	// Set up GORM logger
	//dbLogger := logger.New(
	//	log.New(os.Stdout, "\r\n", log.LstdFlags),
	//	logger.Config{
	//		SlowThreshold: time.Second,
	//		Colorful:      true,
	//		LogLevel:      logger.Info,
	//	},
	//)

	mariaConnect := mariaUser + ":" + mariaPass + "@tcp(" + mariaHost + ":" + "3306" + ")/" + mariaDatabase + "?charset=utf8&parseTime=True&loc=Local"
	db, err := gorm.Open(mysql.New(mysql.Config{
		DSN:                       mariaConnect, // data source name
		DefaultStringSize:         128,          // default size for string fields
		DisableDatetimePrecision:  false,        // disable datetime precision, which not supported before MySQL 5.6
		DontSupportRenameIndex:    true,         // drop & create when rename index, rename index not supported before MySQL 5.7, MariaDB
		DontSupportRenameColumn:   true,         // `change` when rename column, rename column not supported before MySQL 8, MariaDB
		SkipInitializeWithVersion: false,        // auto configure based on currently MySQL version
	}), &gorm.Config{
		//Logger: dbLogger,
	})
	defer func() {
		dbConn, _ := db.DB()
		dbConn.Close()
	}()

	if err != nil {
		log.Fatalf("failed to connect GORM to mysqldb: %s", err.Error())
	}

	// Run auto migrations
	err = db.AutoMigrate(&Record{})
	if err != nil {
		log.Fatalf("failed to migrate databse: %s", err)
	}

	// Last env vars
	salt, err := envy.MustGet("SALT")
	if err != nil {
		log.Fatalf("failed to get salt, you must provide a salt: %s", err)
	}
	envTimeRaw := envy.Get("SESS_TIME", "10")
	envTime, err := strconv.Atoi(envTimeRaw)
	if err != nil {
		envTime = 10
	}
	scaleRaw := envy.Get("SCALE", "1")
	scale, err := strconv.Atoi(scaleRaw)
	if err != nil {
		scale = 1

	}

	log.Println("Setting up worker")

	hitlog := make(chan Hit, 500)
	for i := 0; i < scale; i++ {
		go worker(i, hitlog, salt, db, envTime)
	}

	log.Println("Starting router")

	r := chi.NewRouter()

	// A good base middleware stack
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	//r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.NoCache)
	r.Use(middleware.Heartbeat("/ping"))
	r.Use(middleware.URLFormat)

	// Set a timeout value on the request context (ctx), that will signal
	// through ctx.Done() that the request has timed out and further
	// processing should be stopped.
	r.Use(middleware.Timeout(60 * time.Second))

	t, err := template.ParseFS(assetData, "assets/stats.html")
	if err != nil {
		log.Fatalf("unable to parse template: %w", err)
	}

	r.Get("/snoopy/{slug}", func(w http.ResponseWriter, r *http.Request) {

		ip := strings.Split(r.RemoteAddr, ":")[0]
		slug := chi.URLParam(r, "slug")

		ht := Hit{
			IP:   ip,
			Slug: slug,
		}

		hitlog <- ht
		w.Header().Set("Content-Type", "text/css")
		w.Write([]byte(".snoopy {\n  color: #28a745 !important;\n}"))
	})

	r.Get("/stats", func(w http.ResponseWriter, r *http.Request) {

		var users []StatList
		db.Raw("SELECT COUNT(DISTINCT IP) as users, (YEAR(created_at)*100)+MONTH(created_at) as time FROM records GROUP BY time ORDER BY time").Scan(&users)

		var sessions []StatList
		db.Raw("SELECT COUNT(IP) as users, (YEAR(created_at)*100)+MONTH(created_at) as time FROM records GROUP BY time ORDER BY time").Scan(&sessions)

		var start time.Time
		db.Raw("SELECT created_at from records order by created_at limit 1").Scan(&start)
		start = time.Date(start.Year(), start.Month(), 1, 0, 0, 0, 0, time.UTC)

		slugUsers := make(map[string][]StatList, len(statSlugs))
		slugSessions := make(map[string][]StatList, len(statSlugs))

		for _, v := range statSlugs {
			var pluginUsers []StatList
			db.Raw("SELECT COUNT(DISTINCT IP) as users, (YEAR(created_at)*100)+MONTH(created_at) as time FROM records WHERE slug LIKE @plugin GROUP BY time ORDER BY time",
				sql.Named("plugin", v),
			).Scan(&pluginUsers)

			slugUsers[v] = pluginUsers
			// log.Printf("Plugin %s has %d user entries", v, len(pluginUsers))

			var pluginSessions []StatList
			db.Raw("SELECT COUNT(IP) as users, (YEAR(created_at)*100)+MONTH(created_at) as time FROM records WHERE slug LIKE @plugin GROUP BY time ORDER BY time",
				sql.Named("plugin", v),
			).Scan(&pluginSessions)

			slugSessions[v] = pluginSessions
			// log.Printf("Plugin %s has %d session entries", v, len(pluginSessions))

		}

		sd := StatData{
			FirstRecordDate: start,
			Totals:          users,
			Sessions:        sessions,
			SessionTime:     envTime,
			PluginUsers:     slugUsers,
			PluginSessions:  slugSessions,
		}

		err := t.Execute(w, sd)
		if err != nil {
			panic(err)
		}
	})

	http.ListenAndServe(":3000", r)
	close(hitlog)

	time.Sleep(time.Second)
}

func worker(id int, hits <-chan Hit, salt string, db *gorm.DB, envTime int) {

	hasher := sha512.New()

	for hit := range hits {

		//key := fmt.Sprint(crc32.ChecksumIEEE([]byte(hit.IP + salt)))
		hasher.Reset()
		key := base64.URLEncoding.EncodeToString(hasher.Sum([]byte(hit.IP + salt)))

		rec := Record{}

		res := db.First(&rec, "ip = ? AND slug = ? AND updated_at > ?", key, hit.Slug, time.Now().Add(time.Duration(-1*envTime)*time.Minute))
		if res.Error == gorm.ErrRecordNotFound {
			// Need new record
			rec = Record{
				IP:   key,
				Slug: hit.Slug,
			}

			result := db.Create(&rec)
			if result.Error != nil {
				log.Fatalf("failed to create new record: %s", result.Error)
			}

		} else if res.Error != nil {
			log.Fatalf("query error: %s", res.Error.Error())
		} else {
			// Need to update the existing records last touched time
			db.Save(&rec)
		}
	}
}
