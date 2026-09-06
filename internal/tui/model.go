// Package tui 提供 nmcat 的交互式终端界面（基于 Bubble Tea）：
// 选择存档 → 浏览信息 → 选择操作（解密/加密/导出 .mcworld）→ 确认 → 进度 → 结果。
package tui

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/YuleBest/netease-mc-archive-tool/internal/archive"
	"github.com/YuleBest/netease-mc-archive-tool/internal/crypt"
	"github.com/YuleBest/netease-mc-archive-tool/internal/level"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// ---------------------------------------------------------------- 状态与消息

type screen int

const (
	stateInput   screen = iota // 输入存档路径
	stateDetail                // 存档信息 + 操作列表
	stateConfirm               // 操作确认
	stateRun                   // 执行中（进度）
	stateResult                // 结果
)

const (
	actDecrypt = iota
	actEncrypt
	actExport
	actReopen
	actQuit
	actionCount
)

var actionLabels = []string{
	"解密存档（网易版 → 国际版）",
	"加密存档（国际版 → 网易版）",
	"导出 .mcworld（国际版一键导入）",
	"重新选择存档",
	"退出",
}

type progressMsg struct {
	done, total int
	name        string
}

type jobMsg struct {
	res *archive.Result
	err error
}

type spinnerTickMsg struct{}

// Model 为 TUI 状态机。
type Model struct {
	program *tea.Program // 用于在任务 goroutine 中发送进度消息
	version string

	width, height int
	state         screen
	quitting      bool

	input textinput.Model
	path  string
	store archive.Store

	err    error // 当前页面的错误提示
	stats  *archive.WorldStats
	info   *level.Info
	key    []byte // 预推导密钥（db 已加密时非空）
	keyErr error

	cursor    int
	mode      archive.Mode
	outPath   string
	asZip     bool
	outExists bool

	progressDone, progressTotal int
	progressName                string
	frame                       int

	result *archive.Result
	jobErr error
}

// New 创建初始模型。initialPath 非空时立即加载该存档。
func New(initialPath, version string) Model {
	ti := textinput.New()
	ti.Placeholder = "输入存档 zip 或目录路径，回车确认"
	ti.Focus()
	ti.CharLimit = 512
	// 注意：不要设置 Width。bubbles v0.20.0 的 placeholderView 把占位符的
	// 「显示宽度」（中文按 2 格计）当作 rune 下标去切片，CJK 占位符 + Width>0
	// 必然 panic（slice bounds out of range）。Width=0 走整段渲染的安全分支。
	ti.Width = 0
	m := Model{
		version: version,
		state:   stateInput,
		input:   ti,
	}
	if initialPath != "" {
		ti.SetValue(initialPath)
		m.load(initialPath)
	}
	return m
}

// SetProgram 注入 Program 引用，任务 goroutine 通过它发送进度消息。
func (m *Model) SetProgram(p *tea.Program) { m.program = p }

// Init 实现 tea.Model。
func (m Model) Init() tea.Cmd {
	return tea.Batch(textinput.Blink, m.tick())
}

func (Model) tick() tea.Cmd {
	return tea.Tick(120*time.Millisecond, func(time.Time) tea.Msg { return spinnerTickMsg{} })
}

// ---------------------------------------------------------------- 存档加载

func (m *Model) load(path string) {
	m.err = nil
	m.stats, m.info, m.key, m.keyErr = nil, nil, nil, nil
	m.result, m.jobErr = nil, nil
	m.cursor = 0

	store, err := archive.OpenStore(path)
	if err != nil {
		m.err = err
		return
	}
	m.path, m.store = path, store

	entries, err := store.Entries()
	if err == nil {
		if _, err = archive.LocateDB(entries); err != nil {
			m.err = err
		}
	}
	if m.err != nil {
		m.closeStore()
		return
	}

	if m.stats, err = archive.Inspect(store); err != nil {
		m.err = err
		m.closeStore()
		return
	}
	if m.stats.LevelDatPath != "" {
		if raw, err := store.ReadAll(m.stats.LevelDatPath); err == nil {
			if ld, err := level.ParseLevelDat(raw); err == nil {
				info := ld.Info()
				m.info = &info
			}
		}
	}
	if len(m.stats.EncryptedDB) > 0 && len(m.stats.EncryptedOld) == 0 {
		if key, err := archive.DeriveKey(store); err == nil {
			m.key = key
		} else {
			m.keyErr = err
		}
	}
	m.state = stateDetail
}

func (m *Model) closeStore() {
	if m.store != nil {
		if closer, ok := m.store.(interface{ Close() error }); ok {
			_ = closer.Close()
		}
	}
	m.store = nil
}

// ---------------------------------------------------------------- Update

// Update 实现 tea.Model。
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case spinnerTickMsg:
		if m.state == stateRun {
			m.frame++
			return m, m.tick()
		}
		return m, nil

	case progressMsg:
		m.progressDone, m.progressTotal, m.progressName = msg.done, msg.total, msg.name
		return m, nil

	case jobMsg:
		m.result, m.jobErr = msg.res, msg.err
		m.state = stateResult
		return m, nil

	case tea.KeyMsg:
		if msg.Type == tea.KeyCtrlC {
			m.quitting = true
			m.closeStore()
			return m, tea.Quit
		}
		return m.updateKeys(msg)
	}
	return m, nil
}

func (m Model) updateKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.state {
	case stateInput:
		switch msg.Type {
		case tea.KeyEnter:
			path := strings.TrimSpace(m.input.Value())
			if path == "" {
				m.err = fmt.Errorf("请输入存档路径")
				return m, nil
			}
			m.load(path)
			return m, nil
		case tea.KeyEsc:
			m.quitting = true
			m.closeStore()
			return m, tea.Quit
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd

	case stateDetail:
		switch {
		case msg.Type == tea.KeyUp:
			if m.cursor > 0 {
				m.cursor--
			}
		case msg.Type == tea.KeyDown:
			if m.cursor < actionCount-1 {
				m.cursor++
			}
		case msg.Type == tea.KeyEnter:
			switch m.cursor {
			case actDecrypt, actEncrypt, actExport:
				if m.err = m.prepareOutput(); m.err != nil {
					return m, nil
				}
				m.state = stateConfirm
			case actReopen:
				m.closeStore()
				m.input.SetValue("")
				m.input.Focus()
				m.state = stateInput
			case actQuit:
				m.quitting = true
				m.closeStore()
				return m, tea.Quit
			}
		case msg.Type == tea.KeyEsc, msg.String() == "q":
			m.quitting = true
			m.closeStore()
			return m, tea.Quit
		}
		return m, nil

	case stateConfirm:
		switch msg.Type {
		case tea.KeyEnter:
			m.state = stateRun
			m.progressDone, m.progressTotal, m.progressName = 0, 0, ""
			return m, tea.Batch(m.tick(), m.startJob())
		case tea.KeyEsc:
			m.state = stateDetail
		}
		return m, nil

	case stateResult:
		switch {
		case msg.Type == tea.KeyEnter, msg.Type == tea.KeyEsc:
			m.state = stateDetail
		case msg.String() == "q":
			m.quitting = true
			m.closeStore()
			return m, tea.Quit
		}
		return m, nil
	}
	return m, nil
}

// prepareOutput 根据所选操作计算输出路径；出错时返回错误并停留在详情页。
func (m *Model) prepareOutput() error {
	switch m.cursor {
	case actDecrypt:
		if m.keyErr != nil {
			return m.keyErr
		}
		if m.key == nil && len(m.stats.EncryptedDB) == 0 && m.stats.DBFiles > 0 {
			return fmt.Errorf("存档未加密，无需解密")
		}
		m.mode = archive.ModeDecrypt
		out, _, err := archive.ResolveOutput(m.path, "", "_decrypted")
		m.outPath, m.asZip, m.err = out, strings.HasSuffix(strings.ToLower(out), ".zip"), err
	case actEncrypt:
		m.mode = archive.ModeEncrypt
		out, _, err := archive.ResolveOutput(m.path, "", "_encrypted")
		m.outPath, m.asZip, m.err = out, strings.HasSuffix(strings.ToLower(out), ".zip"), err
	case actExport:
		if m.keyErr != nil {
			return m.keyErr
		}
		m.mode = archive.ModeDecrypt // 导出 = 解密语义 + 世界根提升
		out, err := archive.ResolveExportOutput(m.path, "")
		m.outPath, m.asZip, m.err = out, true, err
	}
	if m.err != nil {
		return m.err
	}
	m.outExists = fileExists(m.outPath)
	return nil
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// startJob 在后台 goroutine 中执行所选操作，进度经 Program.Send 回传。
func (m Model) startJob() tea.Cmd {
	return func() tea.Msg {
		if m.outExists {
			if err := os.Remove(m.outPath); err != nil {
				return jobMsg{err: err}
			}
		}
		sink, err := archive.NewSink(m.outPath, m.asZip)
		if err != nil {
			return jobMsg{err: err}
		}
		target := archive.Store(m.store)
		if m.cursor == actExport {
			inner, err := archive.StripWorldRoot(m.store, archive.WorldRoot(m.stats.DBPrefix))
			if err != nil {
				return jobMsg{err: err}
			}
			target = inner
		}
		key := m.key
		if m.cursor == actEncrypt {
			key = crypt.DefaultKey
		}
		res, err := archive.Transform(target, sink, archive.Options{
			Mode: m.mode,
			Key:  key,
			Progress: func(done, total int, name string) {
				if m.program != nil {
					m.program.Send(progressMsg{done: done, total: total, name: name})
				}
			},
		})
		if err != nil {
			_ = os.Remove(m.outPath)
			return jobMsg{err: err}
		}
		return jobMsg{res: res}
	}
}
