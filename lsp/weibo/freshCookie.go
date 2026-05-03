package weibo

import (
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/cnxysoft/DDBOT-WSa/lsp/cfg"
	"github.com/cnxysoft/DDBOT-WSa/proxy_pool"
	"github.com/cnxysoft/DDBOT-WSa/requests"
	"github.com/cnxysoft/DDBOT-WSa/utils"
	"github.com/guonaihong/gout"
	"github.com/sirupsen/logrus"
)

const (
	// guestCookieRefreshInterval Guest Cookie 刷新的最小间隔
	guestCookieRefreshInterval = 10 * time.Minute
)

var (
	// guestCookieRefreshMu 保护 Guest Cookie 刷新状态
	guestCookieRefreshMu sync.Mutex
	// guestCookieLastRefresh 上次 Guest Cookie 刷新时间
	guestCookieLastRefresh time.Time
)

const (
	pathWeiboCN                = "https://m.weibo.cn/"
	pathWeiboDesktop           = "https://weibo.com"
	pathPassportGenvisitorTest = "https://visitor.passport.weibo.cn/visitor/genvisitor2"
	pathPassportGenvisitorProd = "https://passport.weibo.com/visitor/genvisitor2"
)

var (
	genvisitorRegex = regexp.MustCompile(`\((.*)\)`)
)

func genvisitor(path string, params gout.H, externalOpts ...requests.Option) (*GenVisitorResponse, error) {
	st := time.Now()
	defer func() {
		ed := time.Now()
		logger.WithField("FuncName", utils.FuncName()).Tracef("cost %v", ed.Sub(st))
	}()
	var opts = []requests.Option{
		requests.ProxyOption(proxy_pool.PreferNone),
		requests.AddUAOption(),
		requests.TimeoutOption(time.Second * 10),
	}
	opts = append(opts, externalOpts...)
	var result string
	err := requests.Get(path, params, &result, opts...)
	if err != nil {
		return nil, err
	}
	submatch := genvisitorRegex.FindStringSubmatch(result)
	if len(submatch) < 2 {
		logger.Errorf("genvisitorRegex submatch not found")
		return nil, fmt.Errorf("genvisitor response regex extract failed")
	}
	var resp = new(GenVisitorResponse)
	err = json.Unmarshal([]byte(submatch[1]), resp)
	if err != nil {
		logger.WithField("Content", submatch[1]).Errorf("genvisitor data unmarshal error %v", err)
		resp = nil
	}
	return resp, err
}

func genvisitorGuest(externalOpts ...requests.Option) (*GenVisitorResponse, error) {
	params := gout.H{
		"cb": "gen_callback",
	}
	return genvisitor(pathPassportGenvisitorTest, params, externalOpts...)
}

func genvisitorLogin(externalOpts ...requests.Option) (*GenVisitorResponse, error) {
	params := gout.H{
		"cb":   "visitor_gray_callback",
		"tid":  "",
		"from": "weibo",
	}
	return genvisitor(pathPassportGenvisitorProd, params, externalOpts...)
}

func refreshGuestCN(jar *cookiejar.Jar) error {
	return requests.Get(pathWeiboCN, nil, nil,
		requests.WithCookieJar(jar),
		requests.AddUAOption(),
		requests.ProxyOption(proxy_pool.PreferNone),
		requests.TimeoutOption(time.Second*10),
	)
}

func refreshLoginXsrfToken(jar *cookiejar.Jar) error {
	return requests.Get(pathWeiboDesktop, nil, nil,
		requests.WithCookieJar(jar),
		requests.AddUAOption(),
		requests.ProxyOption(proxy_pool.PreferNone),
		requests.TimeoutOption(time.Second*10),
	)
}

func FreshCookieGuest() ([]*http.Cookie, error) {
	jar, _ := cookiejar.New(nil)
	genVisitorResp, err := genvisitorGuest(requests.WithCookieJar(jar))
	if err != nil {
		logger.Errorf("genvisitor error %v", err)
		return nil, err
	}
	if genVisitorResp.GetRetcode() != 20000000 || !strings.Contains(genVisitorResp.GetMsg(), "succ") {
		logger.WithFields(logrus.Fields{
			"Msg":     genVisitorResp.GetMsg(),
			"Retcode": genVisitorResp.GetRetcode(),
		}).Errorf("incarnateResp error")
		return nil, fmt.Errorf("genvisitor response error %v - %v",
			genVisitorResp.GetRetcode(), genVisitorResp.GetMsg())
	}

	err = refreshGuestCN(jar)
	if err != nil {
		logger.Errorf("refreshGuestMobile error %v", err)
		return nil, err
	}

	cookieUrl, err := url.Parse(pathWeiboCN)
	if err != nil {
		panic(fmt.Sprintf("path %v url parse error", pathWeiboCN))
	}
	return jar.Cookies(cookieUrl), nil
}

func FreshCookieLogin() ([]*http.Cookie, error) {
	jar, _ := cookiejar.New(nil)
	genVisitorResp, err := genvisitorLogin(requests.WithCookieJar(jar))
	if err != nil {
		logger.Errorf("genvisitorLogin error %v", err)
		return nil, err
	}
	if genVisitorResp.GetRetcode() != 20000000 || !strings.Contains(genVisitorResp.GetMsg(), "succ") {
		logger.WithFields(logrus.Fields{
			"Msg":     genVisitorResp.GetMsg(),
			"Retcode": genVisitorResp.GetRetcode(),
		}).Errorf("genvisitorLogin error")
		return nil, fmt.Errorf("genvisitor response error %v - %v",
			genVisitorResp.GetRetcode(), genVisitorResp.GetMsg())
	}

	err = refreshLoginXsrfToken(jar)
	if err != nil {
		logger.Errorf("refreshLoginXsrfToken error %v", err)
		return nil, err
	}

	cookieUrl, err := url.Parse(pathWeiboDesktop)
	if err != nil {
		panic(fmt.Sprintf("path %v url parse error", pathWeiboDesktop))
	}
	return jar.Cookies(cookieUrl), nil
}

func TryRefreshGuestCookie() bool {
	guestCookieRefreshMu.Lock()
	defer guestCookieRefreshMu.Unlock()

	if time.Since(guestCookieLastRefresh) < guestCookieRefreshInterval {
		return true
	}

	cookies, err := FreshCookieGuest()
	if err != nil {
		logger.Errorf("TryRefreshGuestCookie 失败: %v", err)
		return false
	}

	var opt []requests.Option
	for _, cookie := range cookies {
		opt = append(opt, requests.CookieOption(cookie.Name, cookie.Value))
	}
	visitorCookiesOpt.Store(opt)
	guestCookieLastRefresh = time.Now()
	logger.Infof("微博 Guest Cookie 刷新成功，共 %d 个", len(opt))
	return true
}

func ForceFreshCookie() bool {
	refreshMu.Lock()
	defer refreshMu.Unlock()

	if cookieHealthy.Load() {
		return true
	}

	// API 模式不需要 Cookie
	if cfg.IsWeiboAPIMode() {
		cookieHealthy.Store(true)
		consecutiveCookieFails.Store(0)
		return true
	}

	if isGuestMode() {
		// Guest 模式：直接刷新全部 cookie
		cookies, err := FreshCookieGuest()
		if err != nil {
			fails := consecutiveCookieFails.Inc()
			cookieHealthy.Store(false)
			logger.WithField("ConsecutiveFails", fails).Errorf("ForceFreshCookie Guest 失败: %v", err)
			return false
		}
		var opt []requests.Option
		for _, cookie := range cookies {
			opt = append(opt, requests.CookieOption(cookie.Name, cookie.Value))
		}
		visitorCookiesOpt.Store(opt)
		cookieHealthy.Store(true)
		consecutiveCookieFails.Store(0)
		logger.Infof("微博 Guest Cookie 刷新成功")
		return true
	}

	// Login 模式：保留 SUB 和 XSRF-TOKEN，只刷新其他会话 cookie
	currentOpts := CookieOption()
	existingSUB := requests.ExtractCookieOption(currentOpts, "SUB")
	existingXSRF := requests.ExtractCookieOption(currentOpts, "XSRF-TOKEN")

	cookies, err := FreshCookieLogin()
	if err != nil {
		fails := consecutiveCookieFails.Inc()
		cookieHealthy.Store(false)
		logger.WithField("ConsecutiveFails", fails).Errorf("ForceFreshCookie Login 失败: %v", err)
		return false
	}

	// 用 genvisitor 返回的 cookie 做基础，但保留现有的 SUB 和 XSRF-TOKEN
	var opt []requests.Option
	for _, cookie := range cookies {
		if cookie.Name == "SUB" && existingSUB != "" {
			opt = append(opt, requests.CookieOption("SUB", existingSUB))
		} else if cookie.Name == "XSRF-TOKEN" && existingXSRF != "" {
			opt = append(opt, requests.CookieOption("XSRF-TOKEN", existingXSRF))
		} else {
			opt = append(opt, requests.CookieOption(cookie.Name, cookie.Value))
		}
	}
	// 确保 SUB 和 XSRF-TOKEN 不会丢失（genvisitor 响应中通常没有它们）
	if existingSUB != "" {
		hasSUB := false
		for _, c := range cookies {
			if c.Name == "SUB" {
				hasSUB = true
				break
			}
		}
		if !hasSUB {
			opt = append(opt, requests.CookieOption("SUB", existingSUB))
		}
	}
	if existingXSRF != "" {
		hasXSRF := false
		for _, c := range cookies {
			if c.Name == "XSRF-TOKEN" {
				hasXSRF = true
				break
			}
		}
		if !hasXSRF {
			opt = append(opt, requests.CookieOption("XSRF-TOKEN", existingXSRF))
		}
	}

	visitorCookiesOpt.Store(opt)
	cookieHealthy.Store(true)
	consecutiveCookieFails.Store(0)
	logger.Infof("微博 Login Cookie 刷新成功（保留 SUB 和 XSRF-TOKEN）")
	return true
}
