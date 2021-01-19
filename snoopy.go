package main

import (
	"fmt"
	"github.com/go-chi/chi/middleware"
	"gorm.io/gorm/logger"
	"hash/crc32"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi"
	_ "github.com/go-sql-driver/mysql"
	"github.com/gobuffalo/envy"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

type Record struct {
	ID        uint `gorm:"primarykey"`
	CreatedAt time.Time `gorm:"index"`
	UpdatedAt time.Time `gorm:"index"`
	DeletedAt gorm.DeletedAt `gorm:"index"`

	IP string
	Slug string
}

func main() {

	// Load env variables in order to connect to redis and mariadb
	envy.Load()

	// Maria connection!
	mariaHost := envy.Get("MARIA_HOST", "localhost")
	mariaUser := envy.Get("MARIA_USER", "snoopy")
	mariaPass := envy.Get("MARIA_PASS", "snoopy")
	mariaDatabase := envy.Get("MARIA_DATABASE", "snoopy")

	// Set up GORM logger
	dbLogger := logger.New(
		log.New(os.Stdout, "\r\n", log.LstdFlags),
		logger.Config{
			SlowThreshold: time.Second,
			Colorful:      true,
			LogLevel:      logger.Info,
		},
	)

	mariaConnect := mariaUser + ":" + mariaPass + "@tcp(" + mariaHost + ":" + "3306" + ")/" + mariaDatabase + "?charset=utf8&parseTime=True&loc=Local"
	db, err := gorm.Open(mysql.New(mysql.Config{
		DSN: mariaConnect, // data source name
		DefaultStringSize: 128, // default size for string fields
		DisableDatetimePrecision: false, // disable datetime precision, which not supported before MySQL 5.6
		DontSupportRenameIndex: true, // drop & create when rename index, rename index not supported before MySQL 5.7, MariaDB
		DontSupportRenameColumn: true, // `change` when rename column, rename column not supported before MySQL 8, MariaDB
		SkipInitializeWithVersion: false, // auto configure based on currently MySQL version
	}), &gorm.Config{
		Logger: dbLogger,
	})

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
	if err != nil{
		envTime = 10
	}


	r := chi.NewRouter()

	// A good base middleware stack
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.NoCache)
	r.Use(middleware.Heartbeat("/ping"))
	r.Use(middleware.URLFormat)

	// Set a timeout value on the request context (ctx), that will signal
	// through ctx.Done() that the request has timed out and further
	// processing should be stopped.
	r.Use(middleware.Timeout(60 * time.Second))

	r.Get("/snoopy/{slug}", func(w http.ResponseWriter, r *http.Request) {

		ip := strings.Split(r.RemoteAddr, ":")[0]
		log.Println(ip)
		key := fmt.Sprint(crc32.ChecksumIEEE([]byte(ip + salt)))
		slug := chi.URLParam(r, "slug")

		rec := Record{}

		res := db.First(&rec, "ip = ? AND slug = ? AND updated_at > ?", key, slug, time.Now().Add(time.Duration(-1 * envTime) * time.Minute))
		if res.Error == gorm.ErrRecordNotFound {
			// Need new record
			rec = Record{
				IP:        key,
				Slug:      slug,
			}

			result := db.Create(&rec)
			if result.Error != nil {
				log.Fatalf("failed to create new record: %s", err)
			}

		} else if err != nil {
			log.Fatalf("query error: %s", err.Error())
		} else {
			// Need to update the existing records last touched time
			db.Save(&rec)
		}


		w.Write([]byte(".snoopy {\n  color: #28a745 !important;\n}"))
		w.Header().Set("Content-Type", "text/css")
	})


	http.ListenAndServe(":3000", r)
}