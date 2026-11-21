package secrets

import (
	"reflect"
	"testing"
)

func rules(hits []Hit) []string {
	out := []string{}
	for _, h := range hits {
		out = append(out, h.Rule)
	}
	return out
}

func TestScanFindsPasswordInConnectionURL(t *testing.T) {
	hits := Scan("DATABASE_URL=postgresql://app.user:Xk29fLq8vPz1@db.example.net:6543/postgres\n")

	if !reflect.DeepEqual(rules(hits), []string{RuleURLPassword}) {
		t.Fatalf("rules = %v", rules(hits))
	}
	if hits[0].Line != 1 || hits[0].Masked == "Xk29fLq8vPz1" {
		t.Fatalf("hit = %+v; want line 1 and a masked value", hits[0])
	}
}

func TestScanAllowsPlaceholderURLPasswords(t *testing.T) {
	text := "postgres://postgres:postgres@db:5432/app\n" +
		"postgres://user:password@localhost/x\n" +
		"postgres://u:${DB_PASSWORD}@db/x\n" +
		"redis://:<redacted>@cache:6379\n"

	if hits := Scan(text); len(hits) != 0 {
		t.Fatalf("expected no hits, got %+v", hits)
	}
}

func TestScanFindsKnownTokenPrefixes(t *testing.T) {
	text := "OPENAI=sk-proj-abcdEFGHijklMNOPqrst1234\n" +
		"gh: github_pat_11ABCDEFG0123456789abcdef\n"

	if got := rules(Scan(text)); !reflect.DeepEqual(got, []string{RuleTokenPrefix, RuleTokenPrefix}) {
		t.Fatalf("rules = %v", got)
	}
}

func TestScanIgnoresShortPlaceholderTokens(t *testing.T) {
	if hits := Scan(`const apiKey = "sk-proj-xxxxx"` + "\n"); len(hits) != 0 {
		t.Fatalf("expected no hits, got %+v", hits)
	}
}

func TestScanFindsPasswordAssignmentInProse(t *testing.T) {
	hits := Scan("# Default password: Vq7mR2tL9pXw\n")

	if !reflect.DeepEqual(rules(hits), []string{RuleAssignment}) {
		t.Fatalf("rules = %v", rules(hits))
	}
}

func TestScanIgnoresAssignmentPlaceholders(t *testing.T) {
	text := "password: <your-password>\n" +
		"token = ${GITHUB_TOKEN}\n" +
		"API_KEY=your-api-key-here\n" +
		"secret: xxxxxxxxxxxx\n" +
		"password: see .env\n"

	if hits := Scan(text); len(hits) != 0 {
		t.Fatalf("expected no hits, got %+v", hits)
	}
}

func TestRedactJSONMasksByKeyValuePrefixAndURL(t *testing.T) {
	in := map[string]any{
		"env": map[string]any{"OPENAI_API_KEY": "sk-live", "LANG": "en_US.UTF-8"},
		"mcp": map[string]any{"args": []any{"-y", "ghp_abcdefghijklmnopqrst", "postgresql://u:hunter22@h/db"}},
		"on":  true,
	}

	got := RedactJSON(in)

	want := map[string]any{
		"env": map[string]any{"OPENAI_API_KEY": Redacted, "LANG": "en_US.UTF-8"},
		"mcp": map[string]any{"args": []any{"-y", Redacted, "postgresql://u:" + Redacted + "@h/db"}},
		"on":  true,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v\nwant %v", got, want)
	}
}

func TestScanJSONReportsKeyPathsNotValues(t *testing.T) {
	in := map[string]any{
		"env":         map[string]any{"OPENAI_API_KEY": "sk-proj-live-value-123", "LANG": "en_US.UTF-8", "EMPTY_TOKEN": ""},
		"mcpServers":  map[string]any{"pg": map[string]any{"args": []any{"postgresql://u:s3cretPw9@h/db"}}},
		"permissions": map[string]any{"allow": []any{"Bash(gh auth status)"}},
	}

	hits := ScanJSON(in)

	paths := map[string]bool{}
	for _, h := range hits {
		paths[h.Masked] = true
	}
	if len(hits) != 2 || !paths["env.OPENAI_API_KEY"] || !paths["mcpServers.pg.args[]"] {
		t.Fatalf("hits = %+v", hits)
	}
}

func TestRedactJSONDoesNotMutateInput(t *testing.T) {
	in := map[string]any{"GITHUB_TOKEN": "ghp_live"}

	RedactJSON(in)

	if in["GITHUB_TOKEN"] != "ghp_live" {
		t.Fatal("input was mutated")
	}
}

func TestScanIgnoresCodeExpressionsAndDocExamples(t *testing.T) {
	text := "const apiKey = process.env.OPENAI_API_KEY\n" +
		"token: req.headers.authorization\n" +
		"password = os.environ[\"DB_PASSWORD\"]\n" +
		"secret: getSecretFromVault(name)\n" +
		"X-API-Key: sk_live_abc123\n"

	if hits := Scan(text); len(hits) != 0 {
		t.Fatalf("expected no hits, got %+v", hits)
	}
}

func TestScanIgnoresTypeAnnotationsAndVariables(t *testing.T) {
	text := "export function verifyToken(token: string): JWTPayload {\n" +
		"self.headers = {\"X-N8N-API-KEY\": api_key}\n" +
		"def f(password: Optional[str]) -> None:\n"

	if hits := Scan(text); len(hits) != 0 {
		t.Fatalf("expected no hits, got %+v", hits)
	}
}

func TestScanStillFindsRealValueWithClosingBrace(t *testing.T) {
	if hits := Scan(`cfg = {"token": "Qm9x7LpZ2vRt8wNc"}` + "\n"); len(hits) != 1 {
		t.Fatalf("expected one hit, got %+v", hits)
	}
}
