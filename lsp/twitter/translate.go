package twitter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/Sora233/MiraiGo-Template/config"
	"github.com/cnxysoft/DDBOT-WSa/proxy_pool"
	"github.com/cnxysoft/DDBOT-WSa/requests"
)

// TranslationResult represents the translation result
type TranslationResult struct {
	TranslatedText string
	SourceLang     string
	TargetLang     string
	Method         string // "x_api" or "google"
}

// getTranslationBatchWait 返回推送渲染时等待单条预翻译结果的兜底时长。
// 可经 twitter.translate.batchWait（秒）配置；默认对齐翻译最小间隔 + 网络请求耗时预算，
// 覆盖同一批推文中第 2~3 条的翻译，而不是像此前固定 3s 那样只放行第 1 条。
func getTranslationBatchWait() time.Duration {
	if config.GlobalConfig != nil {
		if v := config.GlobalConfig.GetDuration("twitter.translate.batchWait"); v > 0 {
			return v
		}
	}
	return getTranslateMinInterval() + networkRequestBudget
}

// networkRequestBudget 一次翻译请求的网络往返预留时长
const networkRequestBudget = 10 * time.Second

// StartAsyncTranslate 在 fetch 阶段异步预翻译推文（正文与引用，如适用）。
// 结果通过 tweet.translationCh 送达，推送阶段调用 WaitTranslation 读取缓存，
// 避免在 GetMSG 推送路径上同步执行网络翻译阻塞管线。
func StartAsyncTranslate(tweet *Tweet) {
	if tweet == nil || !IsTranslateEnabled() {
		return
	}
	if ShouldTranslate(tweet.Content, tweet.TranslationLang) {
		tweet.translationCh = make(chan *TranslationResult, 1)
		go func(id, content string, ch chan *TranslationResult) {
			ch <- TranslateTweet(context.Background(), id, content)
		}(tweet.ID, tweet.Content, tweet.translationCh)
	}
	if qt := tweet.QuoteTweet; qt != nil && ShouldTranslate(qt.Content, qt.TranslationLang) {
		qt.translationCh = make(chan *TranslationResult, 1)
		go func(id, content string, ch chan *TranslationResult) {
			ch <- TranslateTweet(context.Background(), id, content)
		}(qt.ID, qt.Content, qt.translationCh)
	}
}

// WaitTranslation 等待异步预翻译结果，最多等待 timeout。
// 返回 nil 表示暂无可用结果（未启动翻译或仍在进行中）。
func (t *Tweet) WaitTranslation(timeout time.Duration) *TranslationResult {
	if t == nil || t.translationCh == nil {
		return nil
	}
	select {
	case result := <-t.translationCh:
		return result
	case <-time.After(timeout):
		return nil
	}
}

// 翻译限流器
var (
	translateMu       sync.Mutex
	lastTranslateTime time.Time
)

// waitForTranslateSlot 等待翻译限流窗口。
// 与旧版"间隔不足直接丢弃"不同，这里会阻塞等待直到窗口到来（可被 ctx 取消），
// 保证连续多次翻译调用都能获得配额而不是静默丢弃。
func waitForTranslateSlot(ctx context.Context) bool {
	for {
		translateMu.Lock()
		minInterval := getTranslateMinInterval()
		elapsed := time.Since(lastTranslateTime)
		if elapsed >= minInterval {
			lastTranslateTime = time.Now()
			translateMu.Unlock()
			return true
		}
		remaining := minInterval - elapsed
		translateMu.Unlock()

		select {
		case <-time.After(remaining):
		case <-ctx.Done():
			return false
		}
	}
}

// getTranslateMinInterval 获取翻译最小间隔
func getTranslateMinInterval() time.Duration {
	if config.GlobalConfig != nil {
		interval := config.GlobalConfig.GetDuration("twitter.translate.minInterval")
		if interval > 0 {
			return interval
		}
	}
	return time.Second * 5 // 默认5秒间隔
}

// IsTranslateEnabled 检查翻译功能是否启用
func IsTranslateEnabled() bool {
	if config.GlobalConfig != nil {
		return config.GlobalConfig.GetBool("twitter.translate.enabled")
	}
	return false // 默认关闭
}

// getTranslateTargetLang 获取翻译目标语言，默认 "zh"
func getTranslateTargetLang() string {
	if config.GlobalConfig != nil {
		if lang := strings.TrimSpace(config.GlobalConfig.GetString("twitter.translate.targetLang")); lang != "" {
			return lang
		}
	}
	return "zh"
}

// TranslateTweet translates tweet content to Chinese
// 优先使用 X 官方翻译 API，失败时回退到 Google Translate
func TranslateTweet(ctx context.Context, tweetID, content string) *TranslationResult {
	// 检查翻译功能是否启用
	if !IsTranslateEnabled() {
		return nil
	}

	// 等待限流窗口而不是丢弃，保证多次翻译调用都能获得配额
	if !waitForTranslateSlot(ctx) {
		logger.WithField("TweetId", tweetID).Warn("翻译因等待限流窗口超时/取消而跳过")
		return nil
	}

	// 优先使用 X 官方翻译 API
	if result := translateWithXAPI(ctx, tweetID); result != nil {
		return result
	}

	// 回退到 Google Translate
	if result := translateWithGoogle(content, "auto", getTranslateTargetLang()); result != nil {
		return result
	}

	logger.WithField("TweetId", tweetID).Warn("推文翻译失败（X API 与 Google Translate 均未返回结果）")
	return nil
}

// translateWithXAPI uses X's official translation API (api.x.com/2/grok/translation.json)
func translateWithXAPI(ctx context.Context, tweetID string) *TranslationResult {
	if twitterAPI == nil || !twitterAPI.IsEnabled() {
		return nil
	}

	// 等待限流器
	if !apiWait(ctx) {
		logger.WithField("TweetId", tweetID).Debug("翻译因等待限流器被取消而跳过")
		return nil
	}

	apiURL := "https://api.x.com/2/grok/translation.json"

	var httpCode int

	// 请求体格式: {"content_type":"POST","id":"推文ID","dst_lang":"zh"}
	reqBody := map[string]interface{}{
		"content_type": "POST",
		"id":           tweetID,
		"dst_lang":     getTranslateTargetLang(),
	}

	reqBytes, err := json.Marshal(reqBody)
	if err != nil {
		logger.WithError(err).Debug("Failed to marshal X translation request")
		return nil
	}

	opts := []requests.Option{
		requests.ProxyOption(proxy_pool.PreferOversea),
		requests.TimeoutOption(time.Second * 10),
		requests.AddUAOption(UserAgent),
		requests.HeaderOption("authorization", "Bearer "+twitterAPI.bearerToken),
		requests.HeaderOption("x-csrf-token", twitterAPI.ct0),
		requests.HeaderOption("content-type", "text/plain;charset=UTF-8"),
		requests.HeaderOption("Accept", "*/*"),
		requests.HeaderOption("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8"),
		requests.HeaderOption("Accept-Encoding", "gzip, deflate, br"),
		requests.HeaderOption("sec-ch-ua", `"Chromium";v="149", "Not)A;Brand";v="24"`),
		requests.HeaderOption("sec-ch-ua-mobile", "?0"),
		requests.HeaderOption("sec-ch-ua-platform", `"Windows"`),
		requests.HeaderOption("sec-fetch-dest", "empty"),
		requests.HeaderOption("sec-fetch-mode", "cors"),
		requests.HeaderOption("sec-fetch-site", "same-site"),
		requests.HeaderOption("X-Twitter-Active-User", "yes"),
		requests.HeaderOption("X-Twitter-Auth-Type", "OAuth2Session"),
		requests.HeaderOption("X-Twitter-Client-Language", "zh-cn"),
		requests.HeaderOption("Origin", "https://x.com"),
		requests.HeaderOption("Referer", "https://x.com/"),
		requests.CookieOption("ct0", twitterAPI.ct0),
		requests.CookieOption("auth_token", twitterAPI.authToken),
		requests.RetryOption(1),
		requests.HttpCodeOption(&httpCode),
	}

	var rawResp []byte
	err = requests.PostBody(apiURL, reqBytes, &rawResp, opts...)
	if err != nil {
		if httpCode == http.StatusTooManyRequests {
			api429()
			logger.Debug("X translation API rate limited (429)")
			return nil
		}
		// 不调用 apiError()：翻译端点失败不代表 GraphQL 抓取被封，
		// 计入全局退避会连累正常的 HomeTimeline 抓取
		logger.WithField("TweetId", tweetID).WithError(err).
			Warnf("X 官方翻译 API 请求失败（HTTP %d）", httpCode)
		return nil
	}

	decompressed, err := decompressResponse(rawResp)
	if err != nil {
		logger.WithField("TweetId", tweetID).WithError(err).Warn("X 翻译响应解压失败")
		return nil
	}

	// 响应体命中限速同样按 429 处理
	if isRateLimited(httpCode, decompressed) {
		api429()
		logger.Debug("X translation API rate limited (429)")
		return nil
	}

	// 解析响应：X 翻译 API 返回的是流式多段 JSON，每段形如
	// {"result":{"content_type":"POST","text":"片段","entities":{}}}
	// 需逐个解码并把 text 片段拼接为完整译文
	var (
		resp struct {
			Result struct {
				ContentType string `json:"content_type"`
				Text        string `json:"text"`
			} `json:"result"`
		}
		translated strings.Builder
	)

	dec := json.NewDecoder(bytes.NewReader(decompressed))
	for {
		if err := dec.Decode(&resp); err != nil {
			if err == io.EOF {
				break
			}
			logger.WithField("TweetId", tweetID).WithError(err).
				Warnf("X 翻译响应解析失败: %s", truncateBody(decompressed))
			return nil
		}
		translated.WriteString(resp.Result.Text)
	}

	if translated.Len() == 0 {
		logger.WithField("TweetId", tweetID).
			Warnf("X 翻译返回空文本: %s", truncateBody(decompressed))
		return nil
	}

	apiSuccess()
	logger.Debugf("X API translation successful for tweet %s", tweetID)
	return &TranslationResult{
		TranslatedText: translated.String(),
		SourceLang:     "auto",
		TargetLang:     getTranslateTargetLang(),
		Method:         "x_api",
	}
}

// truncateBody 截断响应体用于日志，避免超长内容刷屏
func truncateBody(body []byte) string {
	const maxLen = 500
	if len(body) > maxLen {
		return string(body[:maxLen]) + "..."
	}
	return string(body)
}

// translateWithGoogle uses Google Translate API (unofficial)
func translateWithGoogle(text, sourceLang, targetLang string) *TranslationResult {
	if text == "" {
		return nil
	}

	// 主端点受限流时回退到备用端点
	endpoints := []string{
		"https://translate.googleapis.com/translate_a/single",
		"https://translate.google.com/translate_a/single",
	}
	for _, endpoint := range endpoints {
		if result := googleTranslateRequest(endpoint, text, sourceLang, targetLang); result != nil {
			return result
		}
	}
	return nil
}

// googleTranslateRequest 请求单个 Google 翻译端点
func googleTranslateRequest(endpoint, text, sourceLang, targetLang string) *TranslationResult {
	params := url.Values{}
	params.Set("client", "gtx")
	params.Set("sl", sourceLang)
	params.Set("tl", targetLang)
	params.Set("dt", "t")
	params.Set("q", text)

	fullURL := fmt.Sprintf("%s?%s", endpoint, params.Encode())

	opts := []requests.Option{
		requests.ProxyOption(proxy_pool.PreferOversea),
		requests.TimeoutOption(time.Second * 10),
		requests.AddUAOption(UserAgent),
		requests.HeaderOption("Accept", "*/*"),
		requests.HeaderOption("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8"),
		requests.HeaderOption("Accept-Encoding", "gzip, deflate, br"),
		requests.HeaderOption("sec-ch-ua", `"Chromium";v="149", "Not)A;Brand";v="24"`),
		requests.HeaderOption("sec-ch-ua-mobile", "?0"),
		requests.HeaderOption("sec-ch-ua-platform", `"Windows"`),
		requests.HeaderOption("Referer", "https://translate.google.com/"),
		requests.HeaderOption("Origin", "https://translate.google.com"),
		requests.RetryOption(1),
	}

	var resp []byte
	err := requests.Get(fullURL, nil, &resp, opts...)
	if err != nil {
		logger.WithError(err).Warnf("Google Translate API 请求失败(%s): %s", endpoint, truncateBody(resp))
		return nil
	}

	// Parse response: [[["translated text","original text",null,null,10]],null,"en"]
	var result []interface{}
	if err := json.Unmarshal(resp, &result); err != nil {
		logger.WithError(err).Warnf("Google Translate 响应解析失败: %s", truncateBody(resp))
		return nil
	}

	if len(result) == 0 {
		logger.Warnf("Google Translate 返回空结果: %s", truncateBody(resp))
		return nil
	}

	// Extract translated text
	translations, ok := result[0].([]interface{})
	if !ok || len(translations) == 0 {
		return nil
	}

	var translatedParts []string
	for _, item := range translations {
		if part, ok := item.([]interface{}); ok && len(part) > 0 {
			if partSeg, ok := part[0].(string); ok {
				translatedParts = append(translatedParts, partSeg)
			}
		}
	}

	translatedText := strings.Join(translatedParts, "")
	if translatedText == "" {
		return nil
	}

	// Detect source language
	sourceLangDetected := "unknown"
	if len(result) > 2 {
		if lang, ok := result[2].(string); ok {
			sourceLangDetected = lang
		}
	}

	logger.Debugf("Google Translate successful: %s -> %s", sourceLangDetected, targetLang)
	return &TranslationResult{
		TranslatedText: translatedText,
		SourceLang:     sourceLangDetected,
		TargetLang:     targetLang,
		Method:         "google",
	}
}

// ShouldTranslate determines if a tweet should be translated
// Returns true if the tweet is not in Chinese
func ShouldTranslate(content, lang string) bool {
	// Skip if already Chinese
	if lang == "zh" || lang == "zh-CN" || lang == "zh-TW" {
		return false
	}

	// Check if content contains Chinese characters
	chineseCount := 0
	totalCount := 0
	for _, r := range content {
		if r >= 0x4E00 && r <= 0x9FFF {
			chineseCount++
		}
		if r != ' ' && r != '\n' && r != '\t' {
			totalCount++
		}
	}

	// If more than 30% is Chinese, skip translation
	if totalCount > 0 && float64(chineseCount)/float64(totalCount) > 0.3 {
		return false
	}

	return true
}
