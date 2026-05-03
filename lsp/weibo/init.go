package weibo

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Sora233/MiraiGo-Template/bot"
	"github.com/Sora233/MiraiGo-Template/config"
	"github.com/cnxysoft/DDBOT-WSa/lsp/cfg"
	"github.com/cnxysoft/DDBOT-WSa/lsp/concern"
	localdb "github.com/cnxysoft/DDBOT-WSa/lsp/buntdb"
	"github.com/cnxysoft/DDBOT-WSa/lsp/mmsg"
	"github.com/cnxysoft/DDBOT-WSa/requests"
	localutils "github.com/cnxysoft/DDBOT-WSa/utils"
	"github.com/cnxysoft/DDBOT-WSa/utils/msgstringer"
	"github.com/tidwall/buntdb"
)

var c *Concern

func init() {
	c = NewConcern(concern.GetNotifyChan())
	concern.RegisterConcern(c)

	// 启动时立即刷新一次 Cookie
	cookieHealthy.Store(false)
	ForceFreshCookie()

	// 每 5 分钟检查 Cookie 健康状态，异常时向所有订阅群发送告警
	go func() {
		var lastAlertSent bool
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			// API 模式不需要检查 Cookie
			if cfg.IsWeiboAPIMode() {
				if lastAlertSent {
					sendCookieAlertToAllGroups(true)
					lastAlertSent = false
				}
				continue
			}

			healthy := cookieHealthy.Load()
			if healthy {
				if lastAlertSent {
					// 从故障中恢复，发送恢复通知
					sendCookieAlertToAllGroups(true)
					lastAlertSent = false
				}
				continue
			}
			// Cookie 不健康：尝试刷新
			refreshed := ForceFreshCookie()
			if refreshed {
				if lastAlertSent {
					// 刷新成功且有此前发过告警，发送恢复通知
					sendCookieAlertToAllGroups(true)
				}
				lastAlertSent = false
			} else if !lastAlertSent {
				// 刷新失败且尚未发过告警，发送告警通知
				sendCookieAlertToAllGroups(false)
				lastAlertSent = true
			}
		}
	}()
}

func freshCookieOpt(sub string) {
	// API 模式不需要刷新 Cookie，直接从外部 API 获取数据
	if cfg.IsWeiboAPIMode() {
		return
	}

	var cookies []*http.Cookie
	var err error

	localutils.Retry(3, time.Second, func() bool {
		if isGuestMode() {
			cookies, err = FreshCookieGuest()
		} else {
			cookies, err = FreshCookieLogin()
		}
		return err == nil
	})

	if err != nil {
		logger.Errorf("FreshCookie error %v", err)
		return
	}

	// 保留所有 Cookie
	opt := []requests.Option{}

	// 确定要使用的 SUB 值
	var useSub string
	if sub != "" {
		useSub = sub
		logger.Infof("使用传入的 SUB 参数：%s...", sub[:min(20, len(sub))])
	} else if configuredSub := GetSettingCookie(); configuredSub != "" {
		useSub = configuredSub
		logger.Infof("使用配置中的 SUB")
	}

	// 构建 Cookie 选项
	for _, cookie := range cookies {
		if cookie.Name == "SUB" && useSub != "" {
			// 使用指定的 SUB 替代
			opt = append(opt, requests.CookieOption("SUB", useSub))
		} else {
			opt = append(opt, requests.CookieOption(cookie.Name, cookie.Value))
		}
	}

	// 如果没有找到 SUB 但需要使用指定的值，手动添加
	if useSub != "" {
		hasSub := false
		for _, cookie := range cookies {
			if cookie.Name == "SUB" {
				hasSub = true
				break
			}
		}
		if !hasSub {
			opt = append(opt, requests.CookieOption("SUB", useSub))
		}
	} else {
		// 记录获取到的 SUB
		for _, cookie := range cookies {
			if cookie.Name == "SUB" {
				logger.Infof("使用获取到的 SUB：%s...", cookie.Value[:min(20, len(cookie.Value))])
				break
			}
		}
	}

	visitorCookiesOpt.Store(opt)
	if isGuestMode() {
		logger.Infof("微博 Guest Cookie 已加载，共 %d 个", len(opt))
	} else {
		logger.Infof("微博 Login Cookie 已加载，共 %d 个", len(opt))
	}
}

func GetSettingCookie() string {
	return config.GlobalConfig.GetString("weibo.sub")
}

func GetQRLoginEnable() bool {
	return config.GlobalConfig.GetBool("weibo.qrlogin")
}

// getBotAdmins 从 buntdb Permission 索引查询所有 bot 管理员 QQ
func getBotAdmins() []int64 {
	var admins []int64
	_ = localdb.RCoverTx(func(tx *buntdb.Tx) error {
		tx.Ascend("Permission", func(key, value string) bool {
			splits := strings.Split(key, ":")
			if len(splits) != 3 || splits[2] != "Admin" {
				return true
			}
			i, err := strconv.ParseInt(splits[1], 0, 64)
			if err != nil {
				logger.WithField("Key", key).Errorf("解析 PermissionKey 失败 %v", err)
			} else {
				admins = append(admins, i)
			}
			return true
		})
		return nil
	})
	return admins
}

// sendCookieAlertToAllGroups 发送 Cookie 告警/恢复通知
// 优先级：bot 管理员私聊 > weibo.alertGroupId（群发）> 不发送
func sendCookieAlertToAllGroups(isRecovery bool) {
	admins := getBotAdmins()

	if len(admins) > 0 {
		for _, qq := range admins {
			sendPrivateAlert(qq, isRecovery)
		}
		return
	}

	// 没有管理员，尝试群发
	groupCode := cfg.GetWeiboAlertGroupId()
	if groupCode > 0 {
		sendGroupAlert(groupCode, isRecovery)
	} else {
		logger.Debug("未找到 bot 管理员且 weibo.alertGroupId 未配置，跳过 Cookie 告警通知")
	}
}

// sendPrivateAlert 私聊发送 Cookie 告警
func sendPrivateAlert(qq int64, isRecovery bool) {
	alertKey := c.StateManager.CookieAlertKey(-qq)
	if !isRecovery {
		err := c.StateManager.Set(alertKey, "",
			localdb.SetExpireOpt(time.Hour*2), localdb.SetNoOverWriteOpt())
		if err != nil {
			logger.WithField("QQ", qq).Debug("微博 Cookie 告警已在 2 小时内发送过，跳过")
			return
		}
	} else {
		_, _ = c.StateManager.Delete(alertKey)
	}

	notify := NewCookieAlertNotify(0, isRecovery)
	m := notify.ToMessage()
	sm := m.ToCombineMessage(mmsg.NewPrivateTarget(qq))
	summary := msgstringer.MsgToString(sm.Elements)

	if bot.Instance == nil || !bot.Instance.Online.Load() {
		logger.WithField("QQ", qq).Warn("Bot 未在线，无法发送私聊告警")
		return
	}
	bot.Instance.SendPrivateMessage(qq, sm, summary)
	logger.WithField("QQ", qq).WithField("IsRecovery", isRecovery).Info("已发送微博 Cookie 告警私聊")
}

// sendGroupAlert 群发 Cookie 告警
func sendGroupAlert(groupCode int64, isRecovery bool) {
	alertKey := c.StateManager.CookieAlertKey(groupCode)
	if !isRecovery {
		err := c.StateManager.Set(alertKey, "",
			localdb.SetExpireOpt(time.Hour*2), localdb.SetNoOverWriteOpt())
		if err != nil {
			logger.WithField("GroupCode", groupCode).
				Debug("微博 Cookie 告警已在 2 小时内发送过，跳过")
			return
		}
	} else {
		_, _ = c.StateManager.Delete(alertKey)
	}
	notify := NewCookieAlertNotify(groupCode, isRecovery)
	concern.GetNotifyChan() <- notify
	logger.WithField("GroupCode", groupCode).
		WithField("IsRecovery", isRecovery).
		Info("已发送微博 Cookie 告警通知")
}
