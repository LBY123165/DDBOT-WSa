package weibo

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Sora233/MiraiGo-Template/bot"
	"github.com/Sora233/MiraiGo-Template/config"
	localdb "github.com/cnxysoft/DDBOT-WSa/lsp/buntdb"
	"github.com/cnxysoft/DDBOT-WSa/lsp/cfg"
	"github.com/cnxysoft/DDBOT-WSa/lsp/concern"
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

			// 检查 SUB 是否有效（仅 Login 模式）
			subValid := true
			if !isGuestMode() {
				sub := GetSettingCookie()
				if sub == "" {
					// 内存中也没有 SUB
					opts := CookieOption()
					sub = extractCookieValue(opts, "SUB")
				}
				if sub != "" {
					subValid = isSUBValid()
				}
			}

			healthy := cookieHealthy.Load()
			if healthy && subValid {
				if lastAlertSent {
					// 从故障中恢复，发送恢复通知
					sendCookieAlertToAllGroups(true)
					lastAlertSent = false
				}
				continue
			}
			// Cookie 不健康或 SUB 失效：尝试刷新
			refreshed := ForceFreshCookie()
			if refreshed && subValid {
				if lastAlertSent {
					// 刷新成功且 SUB 有效，发送恢复通知
					sendCookieAlertToAllGroups(true)
				}
				lastAlertSent = false
			} else if !lastAlertSent {
				// 刷新失败或 SUB 失效且尚未发过告警，发送告警通知
				if cfg.GetWeiboCookieAlertEnabled() {
					sendCookieAlertToAllGroups(false)
					lastAlertSent = true
				}
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

// isSUBValid 检测当前 SUB 是否有效
// 通过调用一个轻量级 API 来验证 Cookie/SUB 是否仍然可用
func isSUBValid() bool {
	testUid := int64(5462373877)
	profileResp, err := ApiContainerGetIndexProfile(testUid)
	if err != nil {
		logger.Debugf("SUB 有效性检测失败 - Profile API: %v", err)
		return false
	}
	if profileResp.GetOk() != 1 {
		logger.Debugf("SUB 有效性检测失败 - Profile API 返回错误码: %v", profileResp.GetOk())
		return false
	}
	return true
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

// broadcastAlertToAllTargets 广播告警到所有目标（管理员私聊 + 已启用告警的群）
func broadcastAlertToAllTargets(sendPrivate func(qq int64), sendGroup func(groupCode int64)) {
	admins := getBotAdmins()
	for _, qq := range admins {
		sendPrivate(qq)
	}
	alertGroups := getEnabledAlertGroups()
	for _, groupCode := range alertGroups {
		sendGroup(groupCode)
	}
	if len(admins) == 0 && len(alertGroups) == 0 {
		logger.Debug("无 bot 管理员且无启用告警的群，跳过告警通知")
	}
}

// sendCookieAlertToAllGroups 发送 Cookie 告警/恢复通知
// 默认私聊发送给管理员，同时发送给已启用告警的群（通过 /config weibo_alert true 开启）
func sendCookieAlertToAllGroups(isRecovery bool) {
	if !cfg.GetWeiboCookieAlertEnabled() {
		logger.Debug("微博 Cookie 告警通知已禁用（weibo.disableCookieAlert=true）")
		return
	}
	broadcastAlertToAllTargets(
		func(qq int64) { sendPrivateAlert(qq, isRecovery) },
		func(groupCode int64) { sendGroupAlert(groupCode, isRecovery) },
	)
}

// sendPrivateAlert 私聊发送 Cookie 告警
// 使用 -qq 作为 key，避免与群号冲突（群号为正数）
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
		// 恢复时清除 SUB 过期告警的去重状态
		clearAlertDedup(c.StateManager.SUBExpiredAlertKey(-qq))
	}

	notify := NewCookieAlertNotify(0, isRecovery)
	m := notify.ToMessage()
	sm := m.ToCombineMessage(mmsg.NewPrivateTarget(qq))
	summary := msgstringer.AdapterMsgToString(sm.Elements)

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
		// 恢复时清除 SUB 过期告警的去重状态
		clearAlertDedup(c.StateManager.SUBExpiredAlertKey(groupCode))
	}

	// 直接发送消息到群，而不是通过通知机制
	// 因为 CookieAlert 类型不在 Types() 返回的列表中，通过通知机制无法发送到群
	notify := NewCookieAlertNotify(groupCode, isRecovery)
	m := notify.ToMessage()
	if bot.Instance != nil && bot.Instance.Online.Load() {
		sm := m.ToCombineMessage(mmsg.NewGroupTarget(groupCode))
		summary := msgstringer.AdapterMsgToString(sm.Elements)
		bot.Instance.SendGroupMessage(groupCode, sm, summary)
		logger.WithField("GroupCode", groupCode).
			WithField("IsRecovery", isRecovery).
			Info("已发送微博 Cookie 告警通知到群")
	} else {
		logger.WithField("GroupCode", groupCode).
			Warn("Bot 未在线，无法发送群告警")
	}
}

// NotifySUBExpired 发送 SUB 过期告警通知
// 默认私聊发送给管理员，同时发送给已启用告警的群
func NotifySUBExpired() {
	if !cfg.GetWeiboCookieAlertEnabled() {
		logger.Debug("微博告警通知已禁用（weibo.disableCookieAlert=true）")
		return
	}
	broadcastAlertToAllTargets(
		sendSUBExpiredAlert,
		sendSUBExpiredGroupAlert,
	)
}

// getEnabledAlertGroups 从数据库读取所有已启用告警的群列表
func getEnabledAlertGroups() []int64 {
	val, err := localdb.Get(localdb.Key("WeiboAlertGroups"))
	if err != nil || val == "" {
		return nil
	}
	var groups []int64
	for _, s := range strings.Split(val, ",") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		groupCode, err := strconv.ParseInt(s, 10, 64)
		if err == nil && groupCode > 0 {
			groups = append(groups, groupCode)
		}
	}
	return groups
}

// enableAlertGroup 启用指定群的 Cookie 告警通知
func enableAlertGroup(groupCode int64) error {
	return localdb.RWCover(func() error {
		groups := getEnabledAlertGroups()
		for _, g := range groups {
			if g == groupCode {
				return nil // 已启用
			}
		}
		groups = append(groups, groupCode)
		strs := make([]string, len(groups))
		for i, g := range groups {
			strs[i] = strconv.FormatInt(g, 10)
		}
		return localdb.Set(localdb.Key("WeiboAlertGroups"), strings.Join(strs, ","))
	})
}

// disableAlertGroup 禁用指定群的 Cookie 告警通知
func disableAlertGroup(groupCode int64) error {
	return localdb.RWCover(func() error {
		groups := getEnabledAlertGroups()
		var filtered []int64
		for _, g := range groups {
			if g != groupCode {
				filtered = append(filtered, g)
			}
		}
		if len(filtered) == 0 {
			_, _ = localdb.Delete(localdb.Key("WeiboAlertGroups"))
			return nil
		}
		strs := make([]string, len(filtered))
		for i, g := range filtered {
			strs[i] = strconv.FormatInt(g, 10)
		}
		return localdb.Set(localdb.Key("WeiboAlertGroups"), strings.Join(strs, ","))
	})
}

// isAlertGroupEnabled 检查指定群是否启用了 Cookie 告警通知
func isAlertGroupEnabled(groupCode int64) bool {
	for _, g := range getEnabledAlertGroups() {
		if g == groupCode {
			return true
		}
	}
	return false
}

// EnableAlertGroup 启用指定群的 Cookie 告警通知（供外部调用）
func EnableAlertGroup(groupCode int64) error {
	return enableAlertGroup(groupCode)
}

// DisableAlertGroup 禁用指定群的 Cookie 告警通知（供外部调用）
func DisableAlertGroup(groupCode int64) error {
	return disableAlertGroup(groupCode)
}

// IsAlertGroupEnabled 检查指定群是否启用了 Cookie 告警通知（供外部调用）
func IsAlertGroupEnabled(groupCode int64) bool {
	return isAlertGroupEnabled(groupCode)
}

// GetBotAdmins 获取所有 bot 管理员 QQ（供外部调用）
func GetBotAdmins() []int64 {
	return getBotAdmins()
}

// GetEnabledAlertGroups 获取所有已启用告警的群列表（供外部调用）
func GetEnabledAlertGroups() []int64 {
	return getEnabledAlertGroups()
}

// trySetAlertDedup 尝试设置告警去重 key，返回 true 表示可以发送告警
// 一小时去重，避免刷屏
func trySetAlertDedup(alertKey string) bool {
	err := c.StateManager.Set(alertKey, "",
		localdb.SetExpireOpt(time.Hour), localdb.SetNoOverWriteOpt())
	if err != nil {
		if localdb.IsRollback(err) {
			logger.Debug("SUB 过期告警已在 1 小时内发送过，跳过")
			return false
		}
		logger.Errorf("设置 SUB 过期告警状态失败: %v", err)
		return false
	}
	return true
}

// clearAlertDedup 清除告警去重 key，用于 Cookie 恢复时重置状态
func clearAlertDedup(alertKey string) {
	c.StateManager.Delete(alertKey)
}

// sendSUBExpiredAlert 私聊发送 SUB 过期告警
// 使用 -qq 作为 key，避免与群号冲突（群号为正数）
func sendSUBExpiredAlert(qq int64) {
	if !trySetAlertDedup(c.StateManager.SUBExpiredAlertKey(-qq)) {
		return
	}

	notify := NewSUBExpiredNotify(0)
	m := notify.ToMessage()
	sm := m.ToCombineMessage(mmsg.NewPrivateTarget(qq))
	summary := msgstringer.AdapterMsgToString(sm.Elements)

	if bot.Instance == nil || !bot.Instance.Online.Load() {
		logger.WithField("QQ", qq).Warn("Bot 未在线，无法发送私聊告警")
		return
	}
	bot.Instance.SendPrivateMessage(qq, sm, summary)
	logger.WithField("QQ", qq).Info("已发送微博 SUB 过期告警私聊")
}

// sendSUBExpiredGroupAlert 群发 SUB 过期告警
func sendSUBExpiredGroupAlert(groupCode int64) {
	if !trySetAlertDedup(c.StateManager.SUBExpiredAlertKey(groupCode)) {
		return
	}

	notify := NewSUBExpiredNotify(groupCode)
	// 直接发送消息到群，而不是通过通知机制
	// 因为 CookieAlert 类型不在 Types() 返回的列表中，通过通知机制无法发送到群
	m := notify.ToMessage()
	if bot.Instance != nil && bot.Instance.Online.Load() {
		sm := m.ToCombineMessage(mmsg.NewGroupTarget(groupCode))
		summary := msgstringer.AdapterMsgToString(sm.Elements)
		bot.Instance.SendGroupMessage(groupCode, sm, summary)
		logger.WithField("GroupCode", groupCode).
			Info("已发送微博 SUB 过期告警通知到群")
	} else {
		logger.WithField("GroupCode", groupCode).
			Warn("Bot 未在线，无法发送群告警")
	}
}
