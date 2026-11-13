import { defineConfig, devices } from "@playwright/test";

export default defineConfig({
	testDir: "./e2e",
	fullyParallel: false,
	use: {
		baseURL: process.env.PLAYWRIGHT_BASE_URL || "http://127.0.0.1:5173",
		launchOptions: process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE
			? { executablePath: process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE }
			: {},
		...devices["Desktop Chrome"],
	},
	webServer: process.env.PLAYWRIGHT_BASE_URL
		? undefined
		: {
				command: "npm run dev -- --host 127.0.0.1",
				url: "http://127.0.0.1:5173",
				reuseExistingServer: !process.env.CI,
			},
});
