// Package workpacket projects registered responsibilities into bounded,
// answerable work. It never changes authorization or lifecycle state.
package workpacket

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"

	"github.com/entroforge/go-system-builder/internal/identity"
	"github.com/entroforge/go-system-builder/internal/schema"
)

type Binding struct {
	RuntimeID             string `json:"runtime_id"`
	Generation            int    `json:"generation"`
	StageEntry            int    `json:"stage_entry"`
	Owner                 string `json:"owner"`
	AssignmentID          string `json:"assignment_id"`
	AssignmentSHA         string `json:"assignment_sha256"`
	Responsibility        string `json:"responsibility"`
	Attempt               int    `json:"attempt"`
	PreviousSubmissionKey string `json:"previous_submission_key,omitempty"`
	RevisionReason        string `json:"revision_reason,omitempty"`
	ExecutionRequired     bool   `json:"execution_required,omitempty"`
}

type Reference struct {
	ID      string `json:"id"`
	Path    string `json:"path"`
	Version string `json:"version"`
	SHA256  string `json:"sha256"`
}

type ReadItem struct {
	Path    string `json:"path"`
	Purpose string `json:"purpose"`
}

type Finding struct {
	Path     string `json:"path"`
	Expected string `json:"expected"`
	Observed string `json:"observed"`
}

type Answer struct {
	Conclusion *string   `json:"conclusion"`
	Findings   []Finding `json:"findings"`
}

type Question struct {
	Question string   `json:"question"`
	Choices  []string `json:"choices,omitempty"`
	Example  *Finding `json:"example,omitempty"`
}

type Draft struct {
	Title       string              `json:"title"`
	Instruction string              `json:"instruction"`
	Read        []ReadItem          `json:"read"`
	Questions   map[string]Question `json:"questions"`
	Answer      Answer              `json:"answer"`
}

type Packet struct {
	TaskAnalysis *TaskAnalysisRef `json:"task_analysis,omitempty"`
	Methods      []ReadItem       `json:"methods,omitempty"`
	ID           string           `json:"packet_id"`
	TemplateID   string           `json:"template_id"`
	TemplateSHA  string           `json:"template_sha256"`
	Binding      Binding          `json:"binding"`
	Inputs       []Reference      `json:"inputs"`
	AnswerPath   string           `json:"answer_path"`
	ViewPath     string           `json:"view_path"`
	Draft        Draft            `json:"draft"`
}

// BindMethods adds the original assignment's reading guidance to a new packet.
// Methods are not reviewed subjects or author fields. Nil preserves old packets
// byte-for-byte; the binding participates in identity and named revisions.
func BindMethods(p Packet, methods []ReadItem) (Packet, error) {
	if len(methods) == 0 {
		return p, nil
	}
	methods = append([]ReadItem(nil), methods...)
	sort.Slice(methods, func(i, j int) bool { return methods[i].Path < methods[j].Path })
	for i, method := range methods {
		path := strings.TrimPrefix(method.Path, ".claude/")
		parts := strings.Split(path, "/")
		if !safeRelative(method.Path) || len(parts) != 3 || parts[0] != "skills" || parts[2] != "SKILL.md" || strings.TrimSpace(method.Purpose) == "" || (i > 0 && methods[i-1].Path == method.Path) {
			return Packet{}, fmt.Errorf("invalid or duplicate S5 method reference")
		}
	}
	data, err := json.Marshal(methods)
	if err != nil {
		return Packet{}, err
	}
	oldID := p.ID
	p.ID = "packet-" + Digest(append([]byte(oldID+":methods:"), data...))
	p.ViewPath = strings.Replace(p.ViewPath, oldID, p.ID, 1)
	p.AnswerPath = strings.Replace(p.AnswerPath, oldID, p.ID, 1)
	p.Methods = methods
	return p, nil
}

type TaskAnalysisRef struct {
	Path          string `json:"path"`
	SHA256        string `json:"sha256"`
	ParserVersion string `json:"parser_version"`
}

// BindTaskAnalysis changes only the frozen knowledge binding, never formal
// subjects or answer authority. Nil preserves the original v1 packet exactly.
func BindTaskAnalysis(p Packet, ref *TaskAnalysisRef) (Packet, error) {
	if ref == nil {
		return p, nil
	}
	if !safeRelative(ref.Path) || ref.Path != ".claude/work-packets/analysis/"+ref.SHA256+".json" || !digestPattern.MatchString(ref.SHA256) || ref.ParserVersion != "1.0.0" {
		return Packet{}, fmt.Errorf("invalid frozen task analysis reference")
	}
	data, err := json.Marshal(ref)
	if err != nil {
		return Packet{}, err
	}
	oldID := p.ID
	p.ID = "packet-" + Digest(append([]byte(oldID+":"), data...))
	p.ViewPath = strings.Replace(p.ViewPath, oldID, p.ID, 1)
	p.AnswerPath = strings.Replace(p.AnswerPath, oldID, p.ID, 1)
	copyRef := *ref
	p.TaskAnalysis = &copyRef
	p.Draft.Read = append(p.Draft.Read, ReadItem{Path: ref.Path, Purpose: "读取这一冻结解析器版本的 TASK 读集/粒度 advisory；它不证明 PASS，也不扩大本责任。"})
	return p, nil
}

func RebuildS5(p Packet) (Packet, error) {
	// Wording updates apply to new questionnaires. Reconstruct an issued
	// packet from its original bundled template, including named revisions.
	for _, name := range []string{"stage-form-s5-template.json", "stage-form-s5-template-save-guidance-legacy.json", "stage-form-s5-template-legacy.json"} {
		data, err := schema.ReadAsset(name)
		if err != nil {
			return Packet{}, fmt.Errorf("read S5 template: %w", err)
		}
		if Digest(data) != p.TemplateSHA {
			continue
		}
		base, err := buildS5(p.Binding, p.Inputs, data)
		if err != nil {
			return Packet{}, err
		}
		base, err = BindMethods(base, p.Methods)
		if err != nil {
			return Packet{}, err
		}
		return BindTaskAnalysis(base, p.TaskAnalysis)
	}
	return Packet{}, fmt.Errorf("unknown issued S5 template fingerprint")
}

func RebuildS5ForBinding(binding Binding, old Packet) (Packet, error) {
	old.Binding = binding
	return RebuildS5(old)
}

type FieldError struct {
	Pointer  string `json:"pointer"`
	Expected string `json:"expected"`
	Observed string `json:"observed"`
	Repair   string `json:"repair"`
}

// AnswerError preserves the original validator's concrete field diagnostics.
// Consumers can distinguish a correctable answer from a source/Writer failure
// without re-reading and re-validating the professional artifact.
type AnswerError struct {
	Label  string
	Fields []FieldError
	Cause  error
}

func (e *AnswerError) Unwrap() error { return e.Cause }

func (e *AnswerError) Error() string {
	if e.Cause != nil {
		return e.Label + ": " + e.Cause.Error()
	}
	if len(e.Fields) == 0 {
		return e.Label
	}
	f := e.Fields[0]
	return fmt.Sprintf("%s %s: %s (%s)", e.Label, f.Pointer, f.Expected, f.Observed)
}

func BuildS5(binding Binding, inputs []Reference) (Packet, error) {
	data, err := schema.ReadAsset("stage-form-s5-template.json")
	if err != nil {
		return Packet{}, fmt.Errorf("read S5 template: %w", err)
	}
	return buildS5(binding, inputs, data)
}

func buildS5(binding Binding, inputs []Reference, data []byte) (Packet, error) {
	if err := identity.ValidateAgentID(binding.Owner); err != nil {
		return Packet{}, fmt.Errorf("work packet owner: %w", err)
	}
	if !safeComponent(binding.Owner) || !safeComponent(binding.RuntimeID) ||
		!safeComponent(binding.AssignmentID) || !digestPattern.MatchString(binding.AssignmentSHA) ||
		binding.Generation < 1 || binding.StageEntry < 0 || binding.Attempt < 1 {
		return Packet{}, fmt.Errorf("invalid work packet binding")
	}
	if binding.Attempt == 1 {
		if binding.PreviousSubmissionKey != "" || binding.RevisionReason != "" || binding.ExecutionRequired {
			return Packet{}, fmt.Errorf("first attempt cannot declare a revision")
		}
	} else if !digestPattern.MatchString(binding.PreviousSubmissionKey) || strings.TrimSpace(binding.RevisionReason) == "" || len(binding.RevisionReason) > 1024 {
		return Packet{}, fmt.Errorf("revision requires a previous submission key and a concrete reason <=1024 bytes")
	}
	var template struct {
		Stage            string `json:"stage"`
		Adapter          string `json:"adapter"`
		Responsibilities map[string]struct {
			Title    string `json:"title"`
			Question string `json:"question"`
		} `json:"responsibilities"`
		Instruction      string   `json:"instruction"`
		Choices          []string `json:"choices"`
		FindingsQuestion string   `json:"findings_question"`
		FindingExample   Finding  `json:"finding_example"`
	}
	if err := json.Unmarshal(data, &template); err != nil {
		return Packet{}, fmt.Errorf("decode S5 template: %w", err)
	}
	question, ok := template.Responsibilities[binding.Responsibility]
	if !ok || template.Stage != "S5" || template.Adapter != "document_review" {
		return Packet{}, fmt.Errorf("unregistered document review responsibility")
	}
	if len(inputs) == 0 {
		return Packet{}, fmt.Errorf("work packet requires frozen inputs")
	}
	inputs = append([]Reference(nil), inputs...)
	sort.Slice(inputs, func(i, j int) bool { return inputs[i].Path < inputs[j].Path })
	seen := map[string]bool{}
	read := make([]ReadItem, 0, len(inputs))
	for _, ref := range inputs {
		if ref.ID == "" || ref.Version == "" || !safeRelative(ref.Path) || !digestPattern.MatchString(ref.SHA256) || seen[ref.Path] {
			return Packet{}, fmt.Errorf("invalid or duplicate frozen document reference")
		}
		seen[ref.Path] = true
		read = append(read, ReadItem{Path: ref.Path, Purpose: "核对本责任的规格、覆盖、依赖和完成条件；结论签署这一冻结版本。"})
	}
	templateSHA := Digest(data)
	idData, err := json.Marshal(struct {
		Binding     Binding
		Inputs      []Reference
		TemplateSHA string
	}{binding, inputs, templateSHA})
	if err != nil {
		return Packet{}, fmt.Errorf("encode packet identity: %w", err)
	}
	id := "packet-" + Digest(idData)
	base := filepath.Join(binding.RuntimeID, fmt.Sprintf("g%d", binding.Generation), binding.Responsibility)
	draft := Draft{Title: question.Title, Instruction: template.Instruction, Read: read, Questions: map[string]Question{
		"conclusion": {Question: question.Question, Choices: template.Choices},
		"findings":   {Question: template.FindingsQuestion, Example: &template.FindingExample},
	}, Answer: Answer{Findings: []Finding{}}}
	return Packet{ID: id, TemplateID: "s5-document-review-v1", TemplateSHA: templateSHA, Binding: binding, Inputs: inputs,
		ViewPath:   filepath.ToSlash(filepath.Join(".claude/work-packets", base, id+".json")),
		AnswerPath: filepath.ToSlash(filepath.Join(".claude/submissions", base, binding.Owner, fmt.Sprintf("a%d-%s", binding.Attempt, id), "answer.json")), Draft: draft}, nil
}

func ValidateAnswer(packet Packet, data []byte) (Answer, []FieldError) {
	issue := func(pointer, expected, observed, repair string) FieldError {
		return FieldError{pointer, expected, observed, repair}
	}
	if len(data) > 256*1024 {
		return Answer{}, []FieldError{issue("/", "JSON <= 256 KiB", "oversized", "只提交当前责任的结论与发现。")}
	}
	if err := rejectDuplicateKeys(data); err != nil {
		return Answer{}, []FieldError{issue("/", "唯一的JSON字段", err.Error(), "删除重复字段后保存。")}
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("stage-answer.schema.json", data); err != nil {
		return Answer{}, []FieldError{issue("/", "注册的答卷结构", err.Error(), "保留机器题干，补齐answer中的必填字段。")}
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var draft Draft
	if err := decoder.Decode(&draft); err != nil {
		return Answer{}, []FieldError{issue("/", "当前问卷JSON", err.Error(), "保留机器题干，修正JSON后保存。")}
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return Answer{}, []FieldError{issue("/", "一份完整JSON", "trailing data", "删除JSON后面的额外内容。")}
	}
	answer := draft.Answer
	draft.Answer = packet.Draft.Answer
	if !reflect.DeepEqual(draft, packet.Draft) {
		return Answer{}, []FieldError{issue("/", "已发行的机器题干", "questionnaire changed", "恢复已发行题干；只编辑answer。")}
	}
	issues := []FieldError{}
	if answer.Conclusion == nil {
		return answer, []FieldError{issue("/answer/conclusion", "pass | fix_required | req_change_required", "null", "完成本项审查后选择结论。")}
	}
	conclusion := *answer.Conclusion
	if conclusion != "pass" && conclusion != "fix_required" && conclusion != "req_change_required" {
		issues = append(issues, issue("/answer/conclusion", "pass | fix_required | req_change_required", conclusion, "使用题目给出的选项。"))
	}
	if answer.Findings == nil || (conclusion == "pass" && len(answer.Findings) > 0) || (conclusion != "pass" && len(answer.Findings) == 0) {
		issues = append(issues, issue("/answer/findings", "pass: []; 其他结论: 至少一项具体发现", "invalid findings", "通过填[]；未通过记录具体问题。"))
	}
	for i, finding := range answer.Findings {
		for _, field := range []struct{ name, value string }{{"path", finding.Path}, {"expected", finding.Expected}, {"observed", finding.Observed}} {
			if strings.TrimSpace(field.value) == "" {
				issues = append(issues, issue(fmt.Sprintf("/answer/findings/%d/%s", i, field.name), "非空的具体观察", field.value, "只补这一格后保存。"))
			}
		}
		path, _, _ := strings.Cut(finding.Path, "#")
		found := false
		for _, ref := range packet.Inputs {
			if path == ref.Path {
				found = true
				break
			}
		}
		if finding.Path != "" && !found {
			issues = append(issues, issue(fmt.Sprintf("/answer/findings/%d/path", i), "当前冻结资料路径及可选anchor", finding.Path, "引用read中的当前被审文档。"))
		}
	}
	return answer, issues
}

// AnswerDigest hashes the validated professional answer, independent of JSON
// formatting and object key order. Callers must validate against the packet first.
func AnswerDigest(answer Answer) (string, error) {
	data, err := json.Marshal(answer)
	if err != nil {
		return "", err
	}
	return Digest(data), nil
}

// JSON decoders normally accept duplicate fields and keep the last value. A
// signed questionnaire must have one unambiguous value for every field.
func rejectDuplicateKeys(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	var readValue func() error
	readValue = func() error {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		delim, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			seen := map[string]bool{}
			for decoder.More() {
				key, err := decoder.Token()
				if err != nil {
					return err
				}
				name, ok := key.(string)
				if !ok || seen[name] {
					return fmt.Errorf("duplicate JSON field %q", name)
				}
				seen[name] = true
				if err := readValue(); err != nil {
					return err
				}
			}
		case '[':
			for decoder.More() {
				if err := readValue(); err != nil {
					return err
				}
			}
		default:
			return fmt.Errorf("unexpected JSON delimiter")
		}
		_, err = decoder.Token()
		return err
	}
	if err := readValue(); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return fmt.Errorf("trailing JSON data")
	}
	return nil
}

var componentPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
var digestPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

func safeComponent(s string) bool { return componentPattern.MatchString(s) }
func safeRelative(s string) bool {
	return s != "" && !filepath.IsAbs(s) && !strings.ContainsAny(s, "\\:#") && filepath.ToSlash(filepath.Clean(s)) == s && s != ".." && !strings.HasPrefix(s, "../")
}

func Digest(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
