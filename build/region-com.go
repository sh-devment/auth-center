package main

var regionCOM = regionDef{
	Lang:     "en",
	Telegram: true,

	Text: pageText{
		Title:      "auth-center",
		SignInWith: "Sign in with:",

		TileGoogle:   "google",
		TileTelegram: "tg",
		TileSolana:   "solana",

		OpenInTelegram: "open in telegram",
		Refresh:        "refresh",

		ConnectWallet: "connect wallet",
		NoWallet:      "no wallet detected —",
		OpenInPhantom: "open in phantom",

		Unavailable: "Sorry, this feature is not available in your region",
		OK:          "ok",

		JS: jsText{
			ConnectWallet:    "connect wallet",
			Connecting:       "connecting...",
			LookingForWallet: "looking for wallet...",
			OpeningWallet:    "opening wallet...",
			Signing:          "signing...",
			Connected:        "connected",
		},
	},
}
