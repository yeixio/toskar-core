package muninn

import "testing"

// Memory commands in the other languages (#718).
func TestParseCommandInOtherLanguages(t *testing.T) {
	cases := []struct {
		in   string
		kind CommandKind
		text string
	}{
		// German
		{"Merk dir, dass ich Tee mag", CommandRemember, "Ich Tee mag"},
		{"Bitte merk dir: meine Tochter hat am 3. Mai Geburtstag", CommandRemember, "Meine Tochter hat am 3. Mai Geburtstag"},
		{"Meine Tochter hat am 3. Mai Geburtstag, merk dir das", CommandRemember, "Meine Tochter hat am 3. Mai Geburtstag"},
		{"Vergiss nicht, dass ich vegetarisch esse", CommandRemember, "Ich vegetarisch esse"},
		{"Vergiss, dass ich Tee mag", CommandForget, "Ich Tee mag"},
		{"Was weißt du über mich?", CommandList, ""},
		{"Merk dir das", CommandNone, ""},
		{"Kannst du dir Namen gut merken?", CommandNone, ""},
		// Spanish
		{"Recuerda que prefiero el sistema métrico", CommandRemember, "Prefiero el sistema métrico"},
		{"Por favor, recuerda que trabajo los sábados", CommandRemember, "Trabajo los sábados"},
		{"Mi hija cumple años el 3 de mayo, recuérdalo", CommandRemember, "Mi hija cumple años el 3 de mayo"},
		{"No olvides que soy alérgico al maní", CommandRemember, "Soy alérgico al maní"},
		{"Olvida que trabajo los sábados", CommandForget, "Trabajo los sábados"},
		{"¿Qué sabes de mí?", CommandList, ""},
		{"Que sabes de mi", CommandList, ""},
		{"¡Recuerda que mañana es feriado!", CommandRemember, "Mañana es feriado"},
		// French
		{"Souviens-toi que je préfère le thé", CommandRemember, "Je préfère le thé"},
		{"Retiens qu'il pleut souvent ici", CommandRemember, "Il pleut souvent ici"},
		{"N’oublie pas que je suis végétarien", CommandRemember, "Je suis végétarien"},
		{"Ma fille est née le 3 mai, retiens-le", CommandRemember, "Ma fille est née le 3 mai"},
		{"Oublie que je préfère le thé", CommandForget, "Je préfère le thé"},
		{"Que sais-tu de moi ?", CommandList, ""},
		{"Retiens ça", CommandNone, ""},
		// Italian
		{"Ricorda che preferisco il tè", CommandRemember, "Preferisco il tè"},
		{"Ricordati che lavoro il sabato", CommandRemember, "Lavoro il sabato"},
		{"Non dimenticare che sono vegetariano", CommandRemember, "Sono vegetariano"},
		{"Dimentica che preferisco il tè", CommandForget, "Preferisco il tè"},
		{"Cosa sai di me?", CommandList, ""},
		{"Ricordare la spesa è difficile", CommandNone, ""},
		// Portuguese
		{"Lembre que prefiro chá", CommandRemember, "Prefiro chá"},
		{"Lembre-se de que trabalho aos sábados", CommandRemember, "Trabalho aos sábados"},
		{"Não esqueça que sou vegetariano", CommandRemember, "Sou vegetariano"},
		{"Esqueça que prefiro chá", CommandForget, "Prefiro chá"},
		{"O que você sabe sobre mim?", CommandList, ""},
		// Japanese
		{"紅茶が好きなことを覚えておいて", CommandRemember, "紅茶が好きなこと"},
		{"娘の誕生日は5月3日だと覚えておいて", CommandRemember, "娘の誕生日は5月3日だ"},
		{"覚えておいて：娘の誕生日は5月3日", CommandRemember, "娘の誕生日は5月3日"},
		{"紅茶が好きなことを忘れて", CommandForget, "紅茶が好きなこと"},
		{"私について何を覚えている？", CommandList, ""},
		{"これを覚えておいて", CommandNone, ""},
		{"覚えておいてくれる？", CommandNone, ""},
		// Korean
		{"나는 차를 좋아한다는 걸 기억해 줘", CommandRemember, "나는 차를 좋아한다는 걸"},
		{"딸 생일은 5월 3일이야, 기억해 줘", CommandRemember, "딸 생일은 5월 3일이야"},
		{"차를 좋아한다는 걸 잊어 줘", CommandForget, "차를 좋아한다는 걸"},
		{"나에 대해 뭘 기억해?", CommandList, ""},
		// Chinese
		{"记住：我喜欢喝茶", CommandRemember, "我喜欢喝茶"},
		{"请记住我女儿的生日是5月3日", CommandRemember, "我女儿的生日是5月3日"},
		{"我喜欢喝茶，记住这一点", CommandRemember, "我喜欢喝茶"},
		{"别忘了我是素食者", CommandRemember, "我是素食者"},
		{"忘掉我喜欢喝茶", CommandForget, "我喜欢喝茶"},
		{"你记得我什么？", CommandList, ""},
		{"记住了吗", CommandNone, ""},
		{"記住：我喜歡喝茶", CommandRemember, "我喜歡喝茶"},
		{"你記得我什麼？", CommandList, ""},
		// Not commands, found in the app's own text (#718): one language's
		// word isn't another's command, and "forgot" isn't "forget".
		{"Guarda cosa fanno i controlli", CommandNone, ""},
		{"Note di rilascio: Toskar", CommandNone, ""},
		{"Guarda esto en el Conocimiento", CommandNone, ""},
		{"忘记密码？请向管理员索取新密码链接。", CommandNone, ""},
		{"忘記密碼？請向管理員索取新密碼連結。", CommandNone, ""},
		// English still works as before.
		{"Remember that I prefer tea", CommandRemember, "I prefer tea"},
		{"What do you remember about me?", CommandList, ""},
	}
	for _, c := range cases {
		got := ParseCommand(c.in)
		if got.Kind != c.kind || got.Text != c.text {
			t.Errorf("ParseCommand(%q) = %q %q, want %q %q", c.in, got.Kind, got.Text, c.kind, c.text)
		}
	}
}
