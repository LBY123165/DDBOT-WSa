package bilibili

import (
	"github.com/cnxysoft/DDBOT-WSa/proxy_pool"
	"github.com/cnxysoft/DDBOT-WSa/requests"
	"github.com/cnxysoft/DDBOT-WSa/utils"
	"time"
)

const (
	// PathGetAttentionList 旧的 /feed/v1/feed/get_attention_list 已被 B 站下线（HTTP 404），
	// 改用 relation/followings/simple：它把当前账号的全部关注直接以 UID 数组返回，
	// data.list 的结构与旧接口完全一致，因此响应结构体可以继续复用。
	PathGetAttentionList = "/x/relation/followings/simple"
)

func GetAttentionList() (*GetAttentionListResponse, error) {
	if !IsVerifyGiven() {
		return nil, ErrVerifyRequired
	}
	st := time.Now()
	defer func() {
		ed := time.Now()
		logger.WithField("FuncName", utils.FuncName()).Tracef("cost %v", ed.Sub(st))
	}()
	url := BPath(PathGetAttentionList)
	var opts []requests.Option
	opts = append(opts,
		requests.ProxyOption(proxy_pool.PreferNone),
		AddUAOption(),
		requests.TimeoutOption(time.Second*10),
		delete412ProxyOption,
	)
	opts = append(opts, GetVerifyOption()...)
	getAttentionListResp := new(GetAttentionListResponse)
	// simple 接口不理会分页参数，一次返回全部关注，无需翻页
	err := requests.Get(url, map[string]interface{}{
		"vmid": accountUid.String(),
	}, getAttentionListResp, opts...)
	if err != nil {
		return nil, err
	}
	return getAttentionListResp, nil
}
