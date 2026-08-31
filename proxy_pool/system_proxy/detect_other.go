//go:build !windows

package system_proxy

import "os"

func detectSystemProxy() (proxy string, enabled bool) {
	return detectUnixProxy()
}

// detectUnixProxy 从环境变量读取代理设置（Linux/macOS）
func detectUnixProxy() (proxy string, enabled bool) {
	// 按优先级检查环境变量
	envVars := []string{"https_proxy", "HTTPS_PROXY", "http_proxy", "HTTP_PROXY", "all_proxy", "ALL_PROXY"}

	for _, envVar := range envVars {
		if value := os.Getenv(envVar); value != "" {
			return normalizeProxyURL(value), true
		}
	}

	return "", false
}
