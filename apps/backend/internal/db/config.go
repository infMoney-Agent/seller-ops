package db

import (
	"bufio"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	// DefaultEnvFile 是 43 服务器默认加载的本机环境配置文件路径（禁止提交到 Git）。
	DefaultEnvFile = "/home/yijiangli/.local/share/seller-ops-gorm-dev/connection.env"

	EnvTest = "test"
	EnvDev  = "dev"
	EnvProd = "prod"
)

// Config 封装 MySQL 与 Redis 的环境化连接配置与连接池参数。
type Config struct {
	AppEnv          string
	EnvFile         string
	MySQLDSN        string
	MySQLDatabase   string
	RedisAddr       string
	RedisPassword   string
	RedisDB         int
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	ConnMaxIdleTime time.Duration
}

// LoadConfig 根据环境变量与可选 ENV_FILE（默认尝试 43 本机 connection.env）加载配置。
func LoadConfig() (Config, error) {
	return LoadConfigWithOptions(os.Getenv("APP_ENV"), os.Getenv("ENV_FILE"))
}

// LoadConfigWithOptions 允许显式指定环境标识与 ENV_FILE 路径（便于 CLI 与测试调用）。
func LoadConfigWithOptions(appEnv, envFile string) (Config, error) {
	fileValues := map[string]string{}
	resolvedEnvFile := strings.TrimSpace(envFile)
	if resolvedEnvFile == "" {
		if _, err := os.Stat(DefaultEnvFile); err == nil {
			resolvedEnvFile = DefaultEnvFile
		}
	}
	if resolvedEnvFile != "" {
		parsed, err := parseEnvFile(resolvedEnvFile)
		if err != nil {
			return Config{}, fmt.Errorf("load env file %s: %w", resolvedEnvFile, err)
		}
		fileValues = parsed
	}

	get := func(key string) string {
		if v := strings.TrimSpace(os.Getenv(key)); v != "" {
			return v
		}
		return strings.TrimSpace(fileValues[key])
	}

	envName := strings.ToLower(strings.TrimSpace(appEnv))
	if envName == "" {
		envName = strings.ToLower(get("APP_ENV"))
	}
	if envName == "" {
		envName = EnvDev
	}
	if envName != EnvTest && envName != EnvDev && envName != EnvProd {
		return Config{}, fmt.Errorf("unsupported APP_ENV %q: must be test, dev, or prod", envName)
	}

	var dsn, dbName string
	var redisDB int
	if envName == EnvTest {
		dsn = get("MYSQL_TEST_DSN")
		dbName = get("MYSQL_TEST_DATABASE")
		if v := get("REDIS_TEST_DB"); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil {
				return Config{}, fmt.Errorf("invalid REDIS_TEST_DB %q: %w", v, err)
			}
			redisDB = n
		} else {
			redisDB = 15
		}
	} else {
		dsn = get("MYSQL_DSN")
		dbName = get("MYSQL_DATABASE")
		if v := get("REDIS_DB"); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil {
				return Config{}, fmt.Errorf("invalid REDIS_DB %q: %w", v, err)
			}
			redisDB = n
		}
	}

	if dsn == "" {
		host := get("MYSQL_HOST")
		port := get("MYSQL_PORT")
		if port == "" {
			port = "3306"
		}
		user := get("MYSQL_USER")
		pass := get("MYSQL_PASSWORD")
		if host != "" && user != "" && dbName != "" {
			dsn = fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=True&loc=Local",
				user, pass, host, port, dbName)
		}
	}
	if dsn == "" {
		return Config{}, fmt.Errorf("MySQL DSN is required for APP_ENV=%s", envName)
	}

	normalizedDSN, extractedDB, err := ensureMySQLDSNParams(dsn)
	if err != nil {
		return Config{}, err
	}
	if dbName == "" {
		dbName = extractedDB
	}

	maxOpen, maxIdle, maxLife, maxIdleTime := defaultPoolConfig(envName)
	if v := get("MYSQL_MAX_OPEN_CONNS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			return Config{}, fmt.Errorf("invalid MYSQL_MAX_OPEN_CONNS %q", v)
		}
		maxOpen = n
	}
	if v := get("MYSQL_MAX_IDLE_CONNS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return Config{}, fmt.Errorf("invalid MYSQL_MAX_IDLE_CONNS %q", v)
		}
		maxIdle = n
	}
	if v := get("MYSQL_CONN_MAX_LIFETIME"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d <= 0 {
			return Config{}, fmt.Errorf("invalid MYSQL_CONN_MAX_LIFETIME %q", v)
		}
		maxLife = d
	}

	return Config{
		AppEnv:          envName,
		EnvFile:         resolvedEnvFile,
		MySQLDSN:        normalizedDSN,
		MySQLDatabase:   dbName,
		RedisAddr:       get("REDIS_ADDR"),
		RedisPassword:   get("REDIS_PASSWORD"),
		RedisDB:         redisDB,
		MaxOpenConns:    maxOpen,
		MaxIdleConns:    maxIdle,
		ConnMaxLifetime: maxLife,
		ConnMaxIdleTime: maxIdleTime,
	}, nil
}

func defaultPoolConfig(envName string) (maxOpen, maxIdle int, maxLifetime, maxIdleTime time.Duration) {
	switch envName {
	case EnvTest:
		return 10, 5, 10 * time.Minute, 5 * time.Minute
	case EnvProd:
		return 60, 20, 30 * time.Minute, 10 * time.Minute
	default:
		return 25, 10, 30 * time.Minute, 10 * time.Minute
	}
}

func parseEnvFile(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	values := map[string]string{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key := strings.TrimSpace(k)
		val := strings.TrimSpace(v)
		if len(val) >= 2 && ((val[0] == '\'' && val[len(val)-1] == '\'') || (val[0] == '"' && val[len(val)-1] == '"')) {
			val = val[1 : len(val)-1]
		}
		values[key] = val
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return values, nil
}

// ensureMySQLDSNParams 确保 DSN 显式包含 charset=utf8mb4、parseTime=True 与 loc。
func ensureMySQLDSNParams(dsn string) (string, string, error) {
	base, rawQuery, hasQuery := strings.Cut(dsn, "?")
	slashIdx := strings.LastIndex(base, "/")
	if slashIdx < 0 || slashIdx == len(base)-1 {
		return "", "", fmt.Errorf("invalid MySQL DSN: missing database name")
	}
	dbName := base[slashIdx+1:]

	params := url.Values{}
	if hasQuery && rawQuery != "" {
		var err error
		params, err = url.ParseQuery(rawQuery)
		if err != nil {
			return "", "", fmt.Errorf("invalid MySQL DSN query parameters: %w", err)
		}
	}
	if params.Get("charset") == "" {
		params.Set("charset", "utf8mb4")
	}
	if strings.ToLower(params.Get("parseTime")) != "true" {
		params.Set("parseTime", "True")
	}
	if params.Get("loc") == "" {
		params.Set("loc", "Local")
	}
	return base + "?" + params.Encode(), dbName, nil
}
