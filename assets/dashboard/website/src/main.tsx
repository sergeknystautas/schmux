import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import App from './App';
import '@dashboard/styles/global.css';
import './styles/website.css';

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <App />
  </StrictMode>
);
