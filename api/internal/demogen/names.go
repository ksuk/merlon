package demogen

import "math/rand"

// nameEntry pairs a Japanese name's kanji spelling with its kana reading and
// a romanized (Hepburn-style) form, used to populate the synthetic
// attributes.name / attributes.name_kana / attributes.name_en fields. Every
// entry here is a generic, extremely common Japanese surname or given name
// (the kind shared by millions of people), never a real specific
// individual's full name — the realNameBlocklist check in selfcheck.go
// additionally rejects any generated full name that happens to collide with
// a hardcoded list of well-known real people or companies.
type nameEntry struct {
	Kanji  string
	Kana   string
	Romaji string
}

// japaneseSurnames lists 100 common Japanese family names.
var japaneseSurnames = []nameEntry{
	{"佐藤", "サトウ", "Sato"}, {"鈴木", "スズキ", "Suzuki"}, {"高橋", "タカハシ", "Takahashi"}, {"田中", "タナカ", "Tanaka"},
	{"伊藤", "イトウ", "Ito"}, {"渡辺", "ワタナベ", "Watanabe"}, {"山本", "ヤマモト", "Yamamoto"}, {"中村", "ナカムラ", "Nakamura"},
	{"小林", "コバヤシ", "Kobayashi"}, {"加藤", "カトウ", "Kato"}, {"吉田", "ヨシダ", "Yoshida"}, {"山田", "ヤマダ", "Yamada"},
	{"佐々木", "ササキ", "Sasaki"}, {"山口", "ヤマグチ", "Yamaguchi"}, {"松本", "マツモト", "Matsumoto"}, {"井上", "イノウエ", "Inoue"},
	{"木村", "キムラ", "Kimura"}, {"林", "ハヤシ", "Hayashi"}, {"斎藤", "サイトウ", "Saito"}, {"清水", "シミズ", "Shimizu"},
	{"山崎", "ヤマザキ", "Yamazaki"}, {"森", "モリ", "Mori"}, {"池田", "イケダ", "Ikeda"}, {"橋本", "ハシモト", "Hashimoto"},
	{"阿部", "アベ", "Abe"}, {"石川", "イシカワ", "Ishikawa"}, {"山下", "ヤマシタ", "Yamashita"}, {"中島", "ナカジマ", "Nakajima"},
	{"石井", "イシイ", "Ishii"}, {"小川", "オガワ", "Ogawa"}, {"前田", "マエダ", "Maeda"}, {"岡田", "オカダ", "Okada"},
	{"長谷川", "ハセガワ", "Hasegawa"}, {"藤田", "フジタ", "Fujita"}, {"後藤", "ゴトウ", "Goto"}, {"近藤", "コンドウ", "Kondo"},
	{"村上", "ムラカミ", "Murakami"}, {"遠藤", "エンドウ", "Endo"}, {"青木", "アオキ", "Aoki"}, {"坂本", "サカモト", "Sakamoto"},
	{"藤井", "フジイ", "Fujii"}, {"西村", "ニシムラ", "Nishimura"}, {"福田", "フクダ", "Fukuda"}, {"太田", "オオタ", "Ota"},
	{"三浦", "ミウラ", "Miura"}, {"藤原", "フジワラ", "Fujiwara"}, {"岡本", "オカモト", "Okamoto"}, {"松田", "マツダ", "Matsuda"},
	{"中川", "ナカガワ", "Nakagawa"}, {"中野", "ナカノ", "Nakano"}, {"原田", "ハラダ", "Harada"}, {"小野", "オノ", "Ono"},
	{"田村", "タムラ", "Tamura"}, {"竹内", "タケウチ", "Takeuchi"}, {"金子", "カネコ", "Kaneko"}, {"和田", "ワダ", "Wada"},
	{"中山", "ナカヤマ", "Nakayama"}, {"石田", "イシダ", "Ishida"}, {"上田", "ウエダ", "Ueda"}, {"森田", "モリタ", "Morita"},
	{"小島", "コジマ", "Kojima"}, {"柴田", "シバタ", "Shibata"}, {"原", "ハラ", "Hara"}, {"宮崎", "ミヤザキ", "Miyazaki"},
	{"酒井", "サカイ", "Sakai"}, {"工藤", "クドウ", "Kudo"}, {"横山", "ヨコヤマ", "Yokoyama"}, {"宮本", "ミヤモト", "Miyamoto"},
	{"内田", "ウチダ", "Uchida"}, {"高木", "タカギ", "Takagi"}, {"安藤", "アンドウ", "Ando"}, {"島田", "シマダ", "Shimada"},
	{"谷口", "タニグチ", "Taniguchi"}, {"大野", "オオノ", "Ono"}, {"高田", "タカダ", "Takada"}, {"丸山", "マルヤマ", "Maruyama"},
	{"今井", "イマイ", "Imai"}, {"河野", "コウノ", "Kono"}, {"田口", "タグチ", "Taguchi"}, {"武田", "タケダ", "Takeda"},
	{"上野", "ウエノ", "Ueno"}, {"杉山", "スギヤマ", "Sugiyama"}, {"大塚", "オオツカ", "Otsuka"}, {"村田", "ムラタ", "Murata"},
	{"平野", "ヒラノ", "Hirano"}, {"新井", "アライ", "Arai"}, {"野口", "ノグチ", "Noguchi"}, {"松井", "マツイ", "Matsui"},
	{"川崎", "カワサキ", "Kawasaki"}, {"渡部", "ワタベ", "Watabe"}, {"菅原", "スガワラ", "Sugawara"}, {"岩崎", "イワサキ", "Iwasaki"},
	{"桜井", "サクライ", "Sakurai"}, {"望月", "モチヅキ", "Mochizuki"}, {"小山", "コヤマ", "Koyama"}, {"服部", "ハットリ", "Hattori"},
	{"川口", "カワグチ", "Kawaguchi"}, {"永井", "ナガイ", "Nagai"}, {"秋山", "アキヤマ", "Akiyama"}, {"平尾", "ヒラオ", "Hirao"},
	{"石橋", "イシバシ", "Ishibashi"}, {"大西", "オオニシ", "Onishi"}, {"菊地", "キクチ", "Kikuchi"}, {"野田", "ノダ", "Noda"},
}

// givenNamesSenior / givenNamesMiddle / givenNamesYoung are 40-entry given
// name pools bucketed by generation, so a customer's date_of_birth and given
// name style stay internally consistent (a 74-year-old and a 21-year-old
// draw from different eras).
var givenNamesSenior = []nameEntry{
	{"清", "キヨシ", "Kiyoshi"}, {"茂", "シゲル", "Shigeru"}, {"実", "ミノル", "Minoru"}, {"勇", "イサム", "Isamu"},
	{"正夫", "マサオ", "Masao"}, {"隆", "タカシ", "Takashi"}, {"博", "ヒロシ", "Hiroshi"}, {"進", "ススム", "Susumu"},
	{"武", "タケシ", "Takeshi"}, {"一郎", "イチロウ", "Ichiro"}, {"和子", "カズコ", "Kazuko"}, {"洋子", "ヨウコ", "Yoko"},
	{"靖子", "ヤスコ", "Yasuko"}, {"文子", "フミコ", "Fumiko"}, {"節子", "セツコ", "Setsuko"}, {"良子", "ヨシコ", "Yoshiko"},
	{"幸子", "サチコ", "Sachiko"}, {"京子", "キョウコ", "Kyoko"}, {"明子", "アキコ", "Akiko"}, {"久子", "ヒサコ", "Hisako"},
	{"正雄", "マサオ", "Masao"}, {"忠雄", "タダオ", "Tadao"}, {"孝", "タカシ", "Takashi"}, {"修", "オサム", "Osamu"},
	{"弘", "ヒロシ", "Hiroshi"}, {"稔", "ミノル", "Minoru"}, {"敏子", "トシコ", "Toshiko"}, {"陽子", "ヨウコ", "Yoko"},
	{"すみれ", "スミレ", "Sumire"}, {"千代", "チヨ", "Chiyo"}, {"信一", "シンイチ", "Shinichi"}, {"忠", "タダシ", "Tadashi"},
	{"次郎", "ジロウ", "Jiro"}, {"三郎", "サブロウ", "Saburo"}, {"喜代美", "キヨミ", "Kiyomi"}, {"礼子", "レイコ", "Reiko"},
	{"貞子", "サダコ", "Sadako"}, {"照子", "テルコ", "Teruko"}, {"勝", "マサル", "Masaru"}, {"豊", "ユタカ", "Yutaka"},
}

var givenNamesMiddle = []nameEntry{
	{"健一", "ケンイチ", "Kenichi"}, {"直樹", "ナオキ", "Naoki"}, {"和也", "カズヤ", "Kazuya"}, {"誠", "マコト", "Makoto"},
	{"卓也", "タクヤ", "Takuya"}, {"大輔", "ダイスケ", "Daisuke"}, {"淳", "ジュン", "Jun"}, {"浩二", "コウジ", "Koji"},
	{"雄二", "ユウジ", "Yuji"}, {"陽一", "ヨウイチ", "Yoichi"}, {"美穂", "ミホ", "Miho"}, {"由美", "ユミ", "Yumi"},
	{"直子", "ナオコ", "Naoko"}, {"恵子", "ケイコ", "Keiko"}, {"真由美", "マユミ", "Mayumi"}, {"香織", "カオリ", "Kaori"},
	{"里美", "サトミ", "Satomi"}, {"智子", "トモコ", "Tomoko"}, {"裕子", "ユウコ", "Yuko"}, {"麻衣", "マイ", "Mai"},
	{"隆之", "タカユキ", "Takayuki"}, {"秀樹", "ヒデキ", "Hideki"}, {"雅人", "マサト", "Masato"}, {"敏之", "トシユキ", "Toshiyuki"},
	{"信也", "シンヤ", "Shinya"}, {"亮", "リョウ", "Ryo"}, {"剛", "ツヨシ", "Tsuyoshi"}, {"孝之", "タカユキ", "Takayuki"},
	{"美香", "ミカ", "Mika"}, {"愛", "アイ", "Ai"}, {"友美", "トモミ", "Tomomi"}, {"久美子", "クミコ", "Kumiko"},
	{"賢治", "ケンジ", "Kenji"}, {"和幸", "カズユキ", "Kazuyuki"}, {"晴美", "ハルミ", "Harumi"}, {"典子", "ノリコ", "Noriko"},
	{"広志", "ヒロシ", "Hiroshi"}, {"真理", "マリ", "Mari"}, {"順子", "ジュンコ", "Junko"}, {"哲也", "テツヤ", "Tetsuya"},
}

var givenNamesYoung = []nameEntry{
	{"翔太", "ショウタ", "Shota"}, {"拓真", "タクマ", "Takuma"}, {"大翔", "ヒロト", "Hiroto"}, {"陸", "リク", "Riku"},
	{"蓮", "レン", "Ren"}, {"悠人", "ユウト", "Yuto"}, {"樹", "イツキ", "Itsuki"}, {"颯太", "ソウタ", "Sota"},
	{"湊", "ミナト", "Minato"}, {"陽翔", "ハルト", "Haruto"}, {"結衣", "ユイ", "Yui"}, {"さくら", "サクラ", "Sakura"},
	{"陽菜", "ヒナ", "Hina"}, {"美咲", "ミサキ", "Misaki"}, {"葵", "アオイ", "Aoi"}, {"凛", "リン", "Rin"},
	{"莉子", "リコ", "Riko"}, {"心愛", "ココア", "Kokoa"}, {"愛梨", "アイリ", "Airi"}, {"麗花", "レイカ", "Reika"},
	{"駿", "シュン", "Shun"}, {"翼", "ツバサ", "Tsubasa"}, {"諒", "リョウ", "Ryo"}, {"直人", "ナオト", "Naoto"},
	{"健太", "ケンタ", "Kenta"}, {"太一", "タイチ", "Taichi"}, {"優斗", "ユウト", "Yuto"}, {"祥太", "ショウタ", "Shota"},
	{"美優", "ミユ", "Miyu"}, {"優花", "ユウカ", "Yuka"}, {"莉緒", "リオ", "Rio"}, {"心結", "ミユ", "Miyu"},
	{"楓", "カエデ", "Kaede"}, {"大和", "ヤマト", "Yamato"}, {"晴翔", "ハルト", "Haruto"}, {"奏太", "ソウタ", "Sota"},
	{"美月", "ミツキ", "Mitsuki"}, {"千尋", "チヒロ", "Chihiro"}, {"知希", "トモキ", "Tomoki"}, {"一輝", "カズキ", "Kazuki"},
}

// foreignNamePool holds first/last name components for a non-Japanese
// nationality. Every entry is a generic first or family name, never a real
// specific individual's full name.
type foreignNamePool struct {
	First []string
	Last  []string
}

var foreignNamePools = map[string]foreignNamePool{
	"PH": {
		First: []string{"Maria", "Juan", "Jose", "Ana", "Rosa", "Rina", "Miguel", "Carmela", "Ramon", "Luz", "Ferdinand", "Grace"},
		Last:  []string{"Santos", "Reyes", "Cruz", "Bautista", "Garcia", "Torres", "Ramos", "Flores", "Mendoza", "Villanueva", "Aquino", "Castillo"},
	},
	"VN": {
		First: []string{"Van", "Thi", "Minh", "Hoang", "Thanh", "Huong", "Duc", "Lan", "Quang", "Mai", "Tuan", "Linh"},
		Last:  []string{"Nguyen", "Tran", "Le", "Pham", "Hoang", "Vu", "Vo", "Dang", "Bui", "Do", "Ho", "Ngo"},
	},
	"NP": {
		First: []string{"Ram", "Sita", "Bikash", "Sunita", "Prakash", "Anita", "Suman", "Kamala", "Deepak", "Puja", "Nabin", "Sarita"},
		Last:  []string{"Sharma", "Thapa", "Gurung", "Shrestha", "Tamang", "Rai", "Magar", "Adhikari", "Karki", "Basnet", "Poudel", "Khadka"},
	},
	"ID": {
		First: []string{"Budi", "Siti", "Andi", "Dewi", "Agus", "Rina", "Hendra", "Wati", "Joko", "Sari", "Bambang", "Yuni"},
		Last:  []string{"Santoso", "Wijaya", "Kusuma", "Pratama", "Setiawan", "Hidayat", "Saputra", "Suryadi", "Wibowo", "Halim", "Gunawan", "Kurniawan"},
	},
	"BR": {
		First: []string{"Carlos", "Ana", "Joao", "Maria", "Pedro", "Fernanda", "Lucas", "Juliana", "Paulo", "Camila", "Rafael", "Beatriz"},
		Last:  []string{"Silva", "Santos", "Oliveira", "Souza", "Costa", "Pereira", "Almeida", "Ribeiro", "Carvalho", "Gomes", "Martins", "Araujo"},
	},
	"CN": {
		First: []string{"Wei", "Fang", "Jun", "Li", "Yan", "Hui", "Tao", "Xin", "Jing", "Lei", "Ying", "Bo"},
		Last:  []string{"Wang", "Li", "Zhang", "Liu", "Chen", "Yang", "Huang", "Zhao", "Wu", "Zhou", "Xu", "Sun"},
	},
	"US": {
		First: []string{"James", "Mary", "Michael", "Jennifer", "Robert", "Linda", "David", "Patricia", "William", "Susan", "Richard", "Karen"},
		Last:  []string{"Anderson", "Miller", "Clark", "Baker", "Turner", "Parker", "Evans", "Collins", "Morgan", "Reed", "Cooper", "Bell"},
	},
	"GB": {
		First: []string{"Oliver", "Emily", "Harry", "Charlotte", "George", "Amelia", "Jack", "Isla", "Thomas", "Grace", "Charlie", "Sophie"},
		Last:  []string{"Smith", "Jones", "Brown", "Taylor", "Wilson", "Evans", "Roberts", "Walker", "White", "Edwards", "Green", "Hughes"},
	},
	"SG": {
		First: []string{"Wei Ling", "Jun Jie", "Hui Min", "Kai Wen", "Xin Yi", "Zhi Hao", "Mei Ling", "Jing Wen", "Wei Jie", "Hui Ling", "Kok Wai", "Siew Fong"},
		Last:  []string{"Tan", "Lim", "Lee", "Ng", "Ong", "Goh", "Chua", "Koh", "Teo", "Yeo", "Sim", "Chong"},
	},
	"KR": {
		First: []string{"Min-jun", "Seo-yeon", "Do-yoon", "Ji-woo", "Ha-eun", "Joon-ho", "Soo-bin", "Yoo-jin", "Tae-yang", "Eun-ji", "Jae-hyun", "Na-eun"},
		Last:  []string{"Kim", "Lee", "Park", "Choi", "Jung", "Kang", "Cho", "Yoon", "Jang", "Lim", "Han", "Oh"},
	},
	"AU": {
		First: []string{"Jack", "Charlotte", "William", "Olivia", "Noah", "Ava", "Oliver", "Mia", "Lucas", "Chloe", "Ethan", "Ruby"},
		Last:  []string{"Wilson", "Taylor", "Thompson", "White", "King", "Scott", "Mitchell", "Turner", "Phillips", "Campbell", "Stewart", "Bennett"},
	},
	"TH": {
		First: []string{"Somchai", "Siriporn", "Somsak", "Malee", "Prasert", "Suda", "Anan", "Ratana", "Kittipong", "Nittaya", "Chai", "Pensri"},
		Last:  []string{"Srisuk", "Chaiyaporn", "Boonmee", "Saetang", "Rattanakosin", "Thongchai", "Wongsawat", "Charoensuk", "Panyarachun", "Kittikorn", "Suksawat", "Amnuay"},
	},
	"AE": {
		First: []string{"Ahmed", "Fatima", "Mohammed", "Aisha", "Khalid", "Mariam", "Omar", "Layla", "Yousef", "Noura", "Hassan", "Salma"},
		Last:  []string{"Al Maktoum", "Al Nahyan", "Al Suwaidi", "Al Marzouqi", "Al Falasi", "Al Shamsi", "Al Zaabi", "Al Kaabi", "Al Mansoori", "Al Ketbi", "Al Hashimi", "Al Blooshi"},
	},
	"MM": {
		First: []string{"Aung", "Thida", "Zaw", "Khin", "Myint", "Nwe", "Kyaw", "Aye", "Thura", "Su", "Htun", "Moe"},
		Last:  []string{"Win", "Htoo", "Naing", "Oo", "Soe", "Tun", "Lwin", "Zin", "Maung", "Thein", "San", "Aung"},
	},
}

// otherGroupCountries is the "他8%" bucket of A3, deliberately excluding
// KP/IR/SY/CU (DD3 / A3: "KP/IR居住顧客は置かない").
var otherGroupCountries = []string{"US", "GB", "SG", "KR", "AU", "TH", "AE", "MM"}

// corridorCountries is A3's コリドー24% bucket.
var corridorCountries = []string{"PH", "VN", "NP", "ID", "BR", "CN"}

// romajiEntry pairs a Japanese word with its romanization.
type romajiEntry struct {
	Word   string
	Romaji string
}

// corporateDomesticStems / corporateDomesticSuffixes combine into synthetic
// Japanese corporate names (法人名パターン合成), e.g. "株式会社アオイ貿易".
var corporateDomesticStems = []romajiEntry{
	{"アオイ", "Aoi"}, {"サクラ", "Sakura"}, {"ミドリ", "Midori"}, {"ヒカリ", "Hikari"}, {"カエデ", "Kaede"},
	{"スミレ", "Sumire"}, {"ツバキ", "Tsubaki"}, {"ハヤブサ", "Hayabusa"}, {"ワカバ", "Wakaba"}, {"ユタカ", "Yutaka"},
	{"コウメイ", "Komei"}, {"フジ", "Fuji"}, {"タイヨウ", "Taiyo"}, {"オリオン", "Orion"}, {"キズナ", "Kizuna"},
	{"ノゾミ", "Nozomi"}, {"アサヒ", "Asahi"}, {"ハルカ", "Haruka"}, {"セイワ", "Seiwa"}, {"コトブキ", "Kotobuki"},
}
var corporateDomesticSuffixes = []romajiEntry{
	{"貿易", "Boeki"}, {"商事", "Shoji"}, {"物流", "Butsuryu"}, {"商会", "Shokai"}, {"興産", "Kosan"},
	{"製作所", "Seisakusho"}, {"フーズ", "Foods"}, {"工業", "Kogyo"}, {"商店", "Shoten"}, {"電機", "Denki"},
}

// npoNames / trustNames / partnershipNames are small fixed pools of fully
// fictitious Japanese entity names for the low-count A3 customer_type
// categories (npo 6 / trust 2 / partnership 2 target).
var npoNames = []string{
	"特定非営利活動法人きずな支援センター", "特定非営利活動法人陽だまり福祉会", "特定非営利活動法人青葉こども未来",
	"特定非営利活動法人はるかぜ地域支援", "特定非営利活動法人みらい国際協力会", "特定非営利活動法人こもれび生活支援",
	"特定非営利活動法人ひだまり高齢者支援", "特定非営利活動法人わかば教育支援", "特定非営利活動法人つばさ災害支援",
	"特定非営利活動法人あすなろ地域振興", "特定非営利活動法人ほほえみ子育て支援", "特定非営利活動法人みどりの森自然保護",
}
var trustNames = []string{
	"あすか家族信託", "ひまわり資産管理信託", "さくら財産承継信託", "みらい家族信託",
	"こもれび資産管理信託", "かえで財産承継信託", "わかば家族信託", "ほたる資産管理信託",
}
var partnershipNames = []string{
	"山川・中野パートナーズ合同会社", "青葉・柏木パートナーズ合同会社", "共栄パートナーズ合同会社",
	"つばさパートナーズ合同会社", "はるかぜパートナーズ合同会社", "みなとパートナーズ合同会社",
	"アライアンス・パートナーズ合同会社", "ノーザンパートナーズ合同会社",
}

// corporateForeignWords1/2 combine into synthetic overseas corporate names,
// e.g. "Meridian Cross Trading Pte. Ltd.".
var corporateForeignWords1 = []string{
	"Meridian", "Pacific", "Golden", "Silver", "Northern", "Eastern", "Summit", "Horizon", "Apex", "Crescent", "Union", "Falcon",
}
var corporateForeignWords2 = []string{
	"Cross Trading", "Logistics", "Holdings", "Capital", "Ventures", "Partners", "Group", "Alliance", "Exchange", "Commerce",
}

func corporateForeignLegalForm(country string) string {
	switch country {
	case "SG":
		return "Pte. Ltd."
	case "GB", "AU":
		return "Ltd."
	case "US":
		return "Inc."
	case "HK":
		return "Ltd."
	default:
		return "Ltd."
	}
}

// pickJapaneseGivenName returns a given name pool entry for the supplied
// generation era ("senior", "middle", "young").
func pickJapaneseGivenName(rng *rand.Rand, era string) nameEntry {
	switch era {
	case "senior":
		return givenNamesSenior[rng.Intn(len(givenNamesSenior))]
	case "young":
		return givenNamesYoung[rng.Intn(len(givenNamesYoung))]
	default:
		return givenNamesMiddle[rng.Intn(len(givenNamesMiddle))]
	}
}

func pickForeignName(rng *rand.Rand, country string) (first, last string) {
	pool, ok := foreignNamePools[country]
	if !ok {
		pool = foreignNamePools["US"]
	}
	return pool.First[rng.Intn(len(pool.First))], pool.Last[rng.Intn(len(pool.Last))]
}
