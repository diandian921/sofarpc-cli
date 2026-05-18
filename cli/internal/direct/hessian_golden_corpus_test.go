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
		name:                 "query-response",
		hex:                  "4fb34865737369616e436f6e747261637448656c706572245175657279526573706f6e736593077375636365737306616d6f756e7404746167736f90544fa46a6176612e6d6174682e426967446563696d616c910576616c75656f910b3131333739352e323438355674001a6a6176612e7574696c2e4172726179732441727261794c6973746e02014101427a",
		wantPresentationJSON: `{"amount":113795.2485,"success":true,"tags":["A","B"]}`,
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
}

const hessianBigIntegerGoldenHex = "4fa46a6176612e6d6174682e426967496e746567657296067369676e756d08626974436f756e74096269744c656e6774680c6c6f776573745365744269741266697273744e6f6e7a65726f496e744e756d036d61676f909190909090567400045b696e746e02497fffffff8f7a"
