package xiaohongshu

import (
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
	"github.com/pkg/errors"
)

// applySchedule 选择“定时发布”并填入目标时间。
func applySchedule(page *rod.Page, when time.Time) error {
	radio, err := findScheduleRadio(page)
	if err != nil {
		return err
	}
	if err := radio.Click(proto.InputMouseButtonLeft, 1); err != nil {
		return err
	}

	time.Sleep(300 * time.Millisecond)

	inputEl, err := findScheduleInput(page)
	if err != nil {
		return err
	}

	// 确保输入框获得焦点再填值
	_ = inputEl.Click(proto.InputMouseButtonLeft, 1)

	whenStr := when.Format("2006-01-02 15:04")
	_, err = inputEl.Eval(`(v) => {
		const el = this;
		el.value = '';
		el.dispatchEvent(new Event('input', { bubbles: true }));
		el.value = v;
		el.dispatchEvent(new Event('input', { bubbles: true }));
		el.dispatchEvent(new Event('change', { bubbles: true }));
	}`, whenStr)
	if err != nil {
		return err
	}

	time.Sleep(300 * time.Millisecond)
	return nil
}

func findScheduleRadio(page *rod.Page) (*rod.Element, error) {
	// 优先通过文本匹配“定时发布”
	if el, err := page.ElementR("label.el-radio", "定时"); err == nil && el != nil {
		return el, nil
	}
	// 退回使用较短的 selector
	if el, err := page.Element("#el-id-3747-47 label.el-radio"); err == nil && el != nil {
		return el, nil
	}
	return nil, errors.New("未找到定时发布单选框")
}

func findScheduleInput(page *rod.Page) (*rod.Element, error) {
	els, err := page.Elements("div.el-date-editor--datetime input")
	if err == nil {
		for _, el := range els {
			if vis, _ := el.Visible(); vis {
				return el, nil
			}
		}
	}
	// 退回使用较短的 selector
	if el, err := page.Element("#el-id-3747-47 input"); err == nil && el != nil {
		return el, nil
	}
	return nil, errors.New("未找到定时发布时间输入框")
}
