import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import '@fontsource/inter/latin-400.css';
import '@fontsource/inter/latin-500.css';
import '@fontsource/inter/latin-600.css';
import '@fontsource/inter/latin-700.css';
import './design/index.css';
import { App } from './app/App';
import { getAdapter } from './data/adapters';
import { boot } from './state/store';

createRoot(document.getElementById('root')!).render(<StrictMode><App /></StrictMode>);
void getAdapter().then(boot);
