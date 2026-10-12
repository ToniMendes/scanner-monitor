package domain

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

type ParametersPDF struct {
	ClassificationName     string
	IsBlankForm            bool
	HolderName             string
	HolderCPF              string
	RecordNumber           string
	HolderRole             string
	IdentificationStatus   string
	IdentificationEvidence string
}

func NewParametersPDF(classificationName string) (*ParametersPDF, error) {
	return (&ParametersPDF{ClassificationName: classificationName}).Normalize()
}

const (
	maxHolderNameLength      = 120
	maxRecordNumberLength    = 40
	maxFolderComponentLength = 180
)

// Normalize validates the classification and returns a copy with only safe,
// explicitly identified holder data. Uncertain handwriting is a valid outcome;
// it must never become a guessed student directory.
func (p *ParametersPDF) Normalize() (*ParametersPDF, error) {
	if p == nil {
		return nil, errors.New("missing document parameters")
	}
	result := *p
	result.ClassificationName = normalizeEnum(result.ClassificationName)
	switch result.ClassificationName {
	case "identidade", "certidao_nascimento", "prontuario", "carteira_vacinacao",
		"historico_escolar", "atestado", "formulario", "documento_aluno", "outro", "documentos_mistos":
	default:
		return nil, fmt.Errorf("invalid classification name: %q", result.ClassificationName)
	}

	result.HolderRole = normalizeEnum(result.HolderRole)
	if result.HolderRole == "" {
		result.HolderRole = "indeterminado"
	}
	switch result.HolderRole {
	case "aluno", "titular", "responsavel", "indeterminado":
	default:
		return nil, fmt.Errorf("invalid holder role: %q", result.HolderRole)
	}

	result.IdentificationStatus = normalizeEnum(result.IdentificationStatus)
	if result.IdentificationStatus == "" {
		result.IdentificationStatus = "ausente"
	}
	switch result.IdentificationStatus {
	case "identificado", "ausente", "ilegivel", "ambiguo":
	default:
		return nil, fmt.Errorf("invalid identification status: %q", result.IdentificationStatus)
	}
	result.IdentificationEvidence = strings.TrimSpace(result.IdentificationEvidence)

	if result.IsBlankForm {
		if result.ClassificationName != "formulario" || strings.TrimSpace(result.HolderName) != "" ||
			strings.TrimSpace(result.HolderCPF) != "" || strings.TrimSpace(result.RecordNumber) != "" ||
			result.IdentificationStatus != "ausente" || result.HolderRole != "indeterminado" ||
			result.IdentificationEvidence != "" {
			return nil, errors.New("blank form contains contradictory classification or holder data")
		}
		result.clearHolder()
		return &result, nil
	}

	if result.IdentificationStatus != "identificado" ||
		(result.HolderRole != "aluno" && result.HolderRole != "titular") {
		if result.IdentificationStatus == "identificado" {
			if result.HolderRole == "responsavel" {
				result.IdentificationStatus = "ausente"
			} else {
				result.IdentificationStatus = "ambiguo"
			}
		}
		result.clearHolder()
		return &result, nil
	}

	name, component := normalizeHolderName(result.HolderName)
	if component == "" || !evidenceIncludesName(result.IdentificationEvidence, name) {
		result.IdentificationStatus = "ambiguo"
		result.clearHolder()
		return &result, nil
	}
	result.HolderName = name
	result.HolderCPF = normalizeCPF(result.HolderCPF)
	if result.HolderCPF != "" && !evidenceIncludesCPF(result.IdentificationEvidence, result.HolderCPF) {
		result.HolderCPF = ""
	}
	result.RecordNumber = normalizeRecordNumber(result.RecordNumber)
	if result.RecordNumber != "" && !evidenceIncludesRecord(result.IdentificationEvidence, result.RecordNumber) {
		result.RecordNumber = ""
	}
	return &result, nil
}

// FolderComponents returns relative, Windows-safe destination components.
// Callers remain responsible for joining these beneath their output directory.
func (p *ParametersPDF) FolderComponents() ([]string, error) {
	normalized, err := p.Normalize()
	if err != nil {
		return nil, err
	}
	if normalized.IsBlankForm {
		return []string{"formularios"}, nil
	}
	if normalized.IdentificationStatus != "identificado" ||
		(normalized.HolderRole != "aluno" && normalized.HolderRole != "titular") {
		return []string{"sem identificação", normalized.ClassificationName}, nil
	}

	_, person := normalizeHolderName(normalized.HolderName)
	if person == "" {
		return []string{"sem identificação", normalized.ClassificationName}, nil
	}
	if normalized.HolderCPF != "" {
		person += "_" + normalized.HolderCPF
	} else if normalized.RecordNumber != "" {
		person += "_prontuario_" + normalized.RecordNumber
	}
	if len(person) > maxFolderComponentLength || !safeComponent(person) {
		return nil, errors.New("holder directory exceeds safe component limits")
	}
	switch normalized.ClassificationName {
	case "identidade", "certidao_nascimento", "prontuario", "documento_aluno":
		return []string{person}, nil
	default:
		return []string{person, normalized.ClassificationName}, nil
	}
}

func (p *ParametersPDF) clearHolder() {
	p.HolderName = ""
	p.HolderCPF = ""
	p.RecordNumber = ""
	p.IdentificationEvidence = ""
}

func normalizeEnum(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func withoutAccents(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.Is(unicode.Mn, r) {
			return -1
		}
		return unicode.ToLower(r)
	}, norm.NFD.String(value))
}

func normalizeHolderName(value string) (string, string) {
	if !utf8.ValidString(value) {
		return "", ""
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", ""
	}
	for _, r := range value {
		if !unicode.IsLetter(r) && !unicode.Is(unicode.Mn, r) && r != ' ' && r != '-' && r != '\'' && r != '’' {
			return "", ""
		}
	}
	name := strings.Join(strings.Fields(value), " ")
	folded := withoutAccents(name)
	for _, unknown := range []string{
		"nao identificado", "nao identificada", "nao informado", "nao informada",
		"sem identificacao", "nome desconhecido", "nome ilegivel", "ilegivel", "desconhecido",
	} {
		if strings.Contains(folded, unknown) {
			return "", ""
		}
	}
	words := strings.Fields(folded)
	completeWords := 0
	for _, word := range words {
		switch word {
		case "nome", "aluno", "responsavel", "titular", "assinatura", "ilegivel", "desconhecido":
			return "", ""
		}
		if word == "de" || word == "da" || word == "do" || word == "das" || word == "dos" || word == "e" {
			continue
		}
		letters := 0
		for _, r := range word {
			if unicode.IsLetter(r) {
				letters++
			}
		}
		if letters < 2 {
			return "", ""
		}
		completeWords++
	}
	if completeWords < 2 {
		return "", ""
	}
	component := strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) {
			return r
		}
		return -1
	}, folded)
	if len(component) > maxHolderNameLength || !safeComponent(component) {
		return "", ""
	}
	return name, component
}

func evidenceIncludesName(evidence, name string) bool {
	if evidence == "" || name == "" {
		return false
	}
	// Preserve word boundaries so a conflicting surname such as "Silvana"
	// cannot substantiate "Silva". Typographic apostrophes and hyphens do not
	// change a person's name, while punctuation and line breaks separate words.
	words := func(value string) string {
		value = strings.Map(func(r rune) rune {
			if unicode.IsLetter(r) {
				return r
			}
			if r == '-' || r == '\'' || r == '’' {
				return -1
			}
			return ' '
		}, withoutAccents(value))
		return " " + strings.Join(strings.Fields(value), " ") + " "
	}
	return strings.Contains(words(evidence), words(name))
}

func normalizeCPF(value string) string {
	var digits strings.Builder
	for _, r := range strings.TrimSpace(value) {
		switch {
		case r >= '0' && r <= '9':
			digits.WriteRune(r)
		case r == '.' || r == '-' || r == ' ':
		default:
			return ""
		}
	}
	cpf := digits.String()
	if len(cpf) != 11 || strings.Count(cpf, cpf[:1]) == 11 {
		return ""
	}
	for position := 9; position < 11; position++ {
		sum := 0
		for index := 0; index < position; index++ {
			sum += int(cpf[index]-'0') * (position + 1 - index)
		}
		check := (sum * 10) % 11
		if check == 10 {
			check = 0
		}
		if int(cpf[position]-'0') != check {
			return ""
		}
	}
	return cpf
}

func evidenceIncludesCPF(evidence, cpf string) bool {
	// Only a whole value attached to the CPF label can substantiate this ID.
	// Unrelated numeric fields and substrings of larger numbers are not evidence.
	for _, match := range cpfEvidencePattern.FindAllStringSubmatch(withoutAccents(evidence), -1) {
		if normalizeCPF(match[1]) == cpf {
			return true
		}
	}
	return false
}

const (
	holderLabelQualifier = `(?:[[:space:]]+(?:do|da|de)[[:space:]]+(?:aluno|aluna|titular|paciente|estudante))`
	numberLabelQualifier = `(?:[[:space:]]+(?:numero|n\.?[º°]|n(?:ro|o)\.?|n\.))`
	identifierLabelEnd   = holderLabelQualifier + `?` + numberLabelQualifier + `?` + holderLabelQualifier + `?` +
		`(?:[[:space:]]+|[[:space:]]*[:=][[:space:]]*)`
)

var (
	cpfEvidencePattern = regexp.MustCompile(`(?:^|[^\pL\pN])cpf` + identifierLabelEnd +
		`([0-9][0-9. -]*)(?:$|[^\pL\pN. -])`)
	recordEvidencePattern = regexp.MustCompile(`(?:^|[^\pL\pN])prontuario` + identifierLabelEnd +
		`([a-z0-9][a-z0-9/_ -]*)(?:$|[^\pL\pN/_.-]|\.(?:$|[^\pL\pN/_.-]))`)
)

func evidenceIncludesRecord(evidence, record string) bool {
	// Compare the entire labeled value; searching only for the candidate could
	// wrongly accept "0012" inside "AB/0012", "0012 2026" or "000012".
	for _, match := range recordEvidencePattern.FindAllStringSubmatch(withoutAccents(evidence), -1) {
		if normalizeRecordNumber(match[1]) == record {
			return true
		}
	}
	return false
}

func normalizeRecordNumber(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return ""
	}
	var result strings.Builder
	hasDigit := false
	separator := false
	slashPending := false
	for _, r := range value {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			if separator && result.Len() > 0 {
				result.WriteByte('-')
			}
			separator = false
			slashPending = false
			result.WriteRune(r)
			if r >= '0' && r <= '9' {
				hasDigit = true
			}
		case r == '-' || r == '_' || r == ' ':
			separator = true
		case r == '/':
			if result.Len() == 0 || slashPending {
				return ""
			}
			separator = true
			slashPending = true
		default:
			return ""
		}
	}
	component := result.String()
	if !hasDigit || slashPending || len(component) > maxRecordNumberLength || !safeComponent(component) {
		return ""
	}
	return component
}

func safeComponent(value string) bool {
	if value == "" || value == "." || value == ".." || strings.TrimRight(value, ". ") != value {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) || strings.ContainsRune("<>:\"/\\|?*", r) {
			return false
		}
	}
	switch strings.ToUpper(value) {
	case "CON", "PRN", "AUX", "NUL", "COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
		"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		return false
	}
	return true
}
