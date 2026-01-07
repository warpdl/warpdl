import { defineConfig } from 'vite';
import { resolve } from 'path';
import { copyFileSync, mkdirSync, existsSync, cpSync } from 'fs';

// Copy static files after build
function copyStaticFiles() {
  return {
    name: 'copy-static-files',
    closeBundle() {
      // Ensure directories exist
      if (!existsSync('dist/popup')) mkdirSync('dist/popup', { recursive: true });
      if (!existsSync('dist/options')) mkdirSync('dist/options', { recursive: true });
      if (!existsSync('dist/assets/icons')) mkdirSync('dist/assets/icons', { recursive: true });

      // Copy manifest
      copyFileSync('manifest.json', 'dist/manifest.json');

      // Copy popup files
      copyFileSync('src/popup/popup.html', 'dist/popup/popup.html');
      copyFileSync('src/popup/popup.css', 'dist/popup/popup.css');

      // Copy options files
      copyFileSync('src/options/options.html', 'dist/options/options.html');
      copyFileSync('src/options/options.css', 'dist/options/options.css');

      // Copy icons
      cpSync('src/assets/icons', 'dist/assets/icons', { recursive: true });
    },
  };
}

export default defineConfig({
  build: {
    outDir: 'dist',
    emptyDirBeforeWrite: true,
    rollupOptions: {
      input: {
        'service-worker': resolve(__dirname, 'src/background/service-worker.ts'),
        'popup/popup': resolve(__dirname, 'src/popup/popup.ts'),
        'options/options': resolve(__dirname, 'src/options/options.ts'),
      },
      output: {
        entryFileNames: '[name].js',
        chunkFileNames: 'chunks/[name]-[hash].js',
        assetFileNames: 'assets/[name]-[hash][extname]',
        format: 'es',
      },
    },
    target: 'esnext',
    minify: false, // Easier debugging during development
    sourcemap: true,
  },
  resolve: {
    alias: {
      '@': resolve(__dirname, 'src'),
      '@shared': resolve(__dirname, 'src/shared'),
      '@background': resolve(__dirname, 'src/background'),
    },
  },
  plugins: [copyStaticFiles()],
});
