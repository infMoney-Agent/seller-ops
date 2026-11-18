package main

import (
	"fmt"
	"net/url"
	"os"
	"strings"
)

type Config struct{ DatabaseURL, Secret, Storage, Address string }

func loadConfig() (Config, error) {
	c := Config{os.Getenv("SCREENING_DATABASE_URL"), os.Getenv("SCREENING_INTERNAL_SECRET"), os.Getenv("SCREENING_STORAGE_DIR"), os.Getenv("SCREENING_ADDR")}
	if c.Storage == "" {
		c.Storage = "/screening-data"
	}
	if c.Address == "" {
		c.Address = ":8090"
	}
	if err := validateDatabase(c.DatabaseURL, os.Getenv("DATABASE_URL"), os.Getenv("AUTH_DATABASE_NAME")); err != nil {
		return c, err
	}
	return c, nil
}

// The screening process must never connect to an authentication database.
func validateDatabase(raw, legacy, authName string) error {
	u, e := url.Parse(raw)
	if e != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") || u.Hostname() == "" {
		return fmt.Errorf("SCREENING_DATABASE_URL must be a PostgreSQL URL")
	}
	name := strings.TrimPrefix(u.Path, "/")
	if name == "" || name == "seller_ops" || name == "casdoor" || (authName != "" && name == authName) {
		return fmt.Errorf("screening requires its own business database")
	}
	if other, e := url.Parse(legacy); e == nil && other.Hostname() == u.Hostname() && other.Port() == u.Port() && other.Path == u.Path {
		return fmt.Errorf("screening and existing service databases must be separate")
	}
	return nil
}
