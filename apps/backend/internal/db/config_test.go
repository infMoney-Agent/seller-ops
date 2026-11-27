package db

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadConfigWithOptions(t *testing.T) {
	tmp := t.TempDir()
	envPath := filepath.Join(tmp, "connection.env")
	content := strings.Join([]string{
		"MYSQL_DSN='user:pass@tcp(127.0.0.1:3306)/seller_ops_gorm_dev'",
		"MYSQL_TEST_DSN='user:pass@tcp(127.0.0.1:3306)/seller_ops_gorm_test?charset=utf8mb4'",
		"REDIS_ADDR=127.0.0.1:6379",
		"REDIS_DB=0",
		"REDIS_TEST_DB=15",
	}, "\n")
	if err := os.WriteFile(envPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	// 清除可能干扰测试的环境变量
	for _, k := range []string{"MYSQL_DSN", "MYSQL_TEST_DSN", "MYSQL_DATABASE", "MYSQL_TEST_DATABASE", "REDIS_DB", "REDIS_TEST_DB"} {
		t.Setenv(k, "")
	}

	testCfg, err := LoadConfigWithOptions(EnvTest, envPath)
	if err != nil {
		t.Fatalf("load test config: %v", err)
	}
	if testCfg.MySQLDatabase != "seller_ops_gorm_test" || testCfg.RedisDB != 15 {
		t.Fatalf("unexpected test config: %+v", testCfg)
	}
	if !strings.Contains(testCfg.MySQLDSN, "charset=utf8mb4") || !strings.Contains(testCfg.MySQLDSN, "parseTime=True") {
		t.Fatalf("expected normalized DSN params, got %s", testCfg.MySQLDSN)
	}

	devCfg, err := LoadConfigWithOptions(EnvDev, envPath)
	if err != nil {
		t.Fatalf("load dev config: %v", err)
	}
	if devCfg.MySQLDatabase != "seller_ops_gorm_dev" || devCfg.RedisDB != 0 {
		t.Fatalf("unexpected dev config: %+v", devCfg)
	}
	if devCfg.ConnMaxLifetime.Minutes() != 30 {
		t.Fatalf("expected 30m ConnMaxLifetime for dev, got %v", devCfg.ConnMaxLifetime)
	}
}
