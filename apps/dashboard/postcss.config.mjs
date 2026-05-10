import path from 'node:path';
import { fileURLToPath } from 'node:url';
import tailwindcss from 'tailwindcss';
import autoprefixer from 'autoprefixer';

const __dirname = path.dirname(fileURLToPath(import.meta.url));

/** Explicit `config` so Tailwind resolves `content` correctly when Vite cwd is `apps/electron`. */
export default {
  plugins: [tailwindcss({ config: path.join(__dirname, 'tailwind.config.ts') }), autoprefixer()],
};
