import next from "eslint-config-next/core-web-vitals";

const config = [{ ignores: [".next/**", "node_modules/**", "coverage/**"] }, ...next];

export default config;
