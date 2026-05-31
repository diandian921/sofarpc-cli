package direct

type hessianGoldenCase struct {
	name                 string
	hex                  string
	wantPresentationJSON string
}

var hessianJavaGoldenCases = []hessianGoldenCase{
	{
		name:                 "string-emoji",
		hex:                  "0461eda0bdedb98262",
		wantPresentationJSON: `"a🙂b"`,
	},
	{
		name:                 "long",
		hex:                  "4c06058ae04ec22000",
		wantPresentationJSON: `433905635109773312`,
	},
	{
		name:                 "integer",
		hex:                  "95",
		wantPresentationJSON: `5`,
	},
	{
		name:                 "double-whole",
		hex:                  "6902",
		wantPresentationJSON: `2`,
	},
	{
		name:                 "big-decimal",
		hex:                  "4fa46a6176612e6d6174682e426967446563696d616c910576616c75656f9007313030302e3530",
		wantPresentationJSON: `1000.50`,
	},
	{
		name:                 "list-with-null",
		hex:                  "566e034ee10374776f7a",
		wantPresentationJSON: `[null,1,"two"]`,
	},
	{
		name:                 "map-long-key",
		hex:                  "4d7400176a6176612e7574696c2e4c696e6b6564486173684d6170e705736576656e046e616d6505616c6963657a",
		wantPresentationJSON: `{"7":"seven","name":"alice"}`,
	},
	{
		name:                 "bytes",
		hex:                  "230102ff",
		wantPresentationJSON: `"AQL/"`,
	},
	{
		name:                 "enum",
		hex:                  "4fac4865737369616e436f6e747261637448656c7065722453746174757391046e616d656f9006414354495645",
		wantPresentationJSON: `{"name":"ACTIVE"}`,
	},
	{
		name:                 "query-response",
		hex:                  "4fb34865737369616e436f6e747261637448656c706572245175657279526573706f6e736593077375636365737306616d6f756e7404746167736f90544fa46a6176612e6d6174682e426967446563696d616c910576616c75656f910b3131333739352e323438355674001a6a6176612e7574696c2e4172726179732441727261794c6973746e02014101427a",
		wantPresentationJSON: `{"amount":113795.2485,"success":true,"tags":["A","B"]}`,
	},
	{
		name:                 "enum-response",
		hex:                  "4fb24865737369616e436f6e747261637448656c70657224456e756d526573706f6e7365920673746174757307686973746f72796f904fac4865737369616e436f6e747261637448656c7065722453746174757391046e616d656f91064143544956455674001a6a6176612e7574696c2e4172726179732441727261794c6973746e016f9108494e4143544956457a",
		wantPresentationJSON: `{"history":[{"name":"INACTIVE"}],"status":{"name":"ACTIVE"}}`,
	},
	{
		name:                 "nested-response",
		hex:                  "4fb54865737369616e436f6e747261637448656c70657224436f6d706c6578526573706f6e736594077072696d61727907686973746f72790a61747472696275746573056d697865646f904fb34865737369616e436f6e747261637448656c706572245175657279526573706f6e736593077375636365737306616d6f756e7404746167736f91544fa46a6176612e6d6174682e426967446563696d616c910576616c75656f9204312e32335674001a6a6176612e7574696c2e4172726179732441727261794c6973746e0101507a7690916f91466f9204302e303076909101484d7400176a6176612e7574696c2e4c696e6b6564486173684d6170066d70436f64654c06058ae04ec22000086e756c6c61626c654e05726174696f69027a566e034e0178e97a",
		wantPresentationJSON: `{"attributes":{"mpCode":433905635109773312,"nullable":null,"ratio":2},"history":[{"amount":0.00,"success":false,"tags":["H"]}],"mixed":[null,"x",9],"primary":{"amount":1.23,"success":true,"tags":["P"]}}`,
	},
	{
		name:                 "date",
		hex:                  "640000000000000000",
		wantPresentationJSON: `0`,
	},
	{
		name:                 "set",
		hex:                  "567400176a6176612e7574696c2e4c696e6b6564486173685365746e0301780179017a7a",
		wantPresentationJSON: `["x","y","z"]`,
	},
	{
		name:                 "local-date",
		hex:                  "4fba636f6d2e63617563686f2e6865737369616e2e696f2e6a646b382e4c6f63616c4461746548616e646c65930479656172056d6f6e7468036461796f90cfe8919f",
		wantPresentationJSON: `"2024-01-15"`,
	},
	{
		name:                 "local-date-time",
		hex:                  "4fbe636f6d2e63617563686f2e6865737369616e2e696f2e6a646b382e4c6f63616c4461746554696d6548616e646c659204646174650474696d656f904fba636f6d2e63617563686f2e6865737369616e2e696f2e6a646b382e4c6f63616c4461746548616e646c65930479656172056d6f6e7468036461796f91cfe8919f4fba636f6d2e63617563686f2e6865737369616e2e696f2e6a646b382e4c6f63616c54696d6548616e646c659404686f7572066d696e757465067365636f6e64046e616e6f6f929aae9090",
		wantPresentationJSON: `"2024-01-15T10:30:00"`,
	},
	{
		name:                 "instant",
		hex:                  "4fb8636f6d2e63617563686f2e6865737369616e2e696f2e6a646b382e496e7374616e7448616e646c6592077365636f6e6473056e616e6f736f907765a5092890",
		wantPresentationJSON: `"2024-01-15T10:30:00Z"`,
	},
}

const hessianBigIntegerGoldenHex = "4fa46a6176612e6d6174682e426967496e746567657296067369676e756d08626974436f756e74096269744c656e6774680c6c6f776573745365744269741266697273744e6f6e7a65726f496e744e756d036d61676f909190909090567400045b696e746e02497fffffff8f7a"

// hessianCircularGoldenHex is a two-node self-referential graph (a.next=b,
// b.next=a) — kept out of the presentation-based corpus because a cyclic value
// cannot be rendered to JSON. TestHessianGoldenCircularReferenceResolves pins that
// our reader resolves the Hessian back-reference into a shared object.
const hessianCircularGoldenHex = "4faa4865737369616e436f6e747261637448656c706572244e6f646592046e616d65046e6578746f9001616f9001624a00"
