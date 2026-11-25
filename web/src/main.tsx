import React from 'react'
import ReactDOM from 'react-dom/client'
import App from './App.tsx'
import { Toaster } from 'sonner'
import './index.css'
import { BrowserRouter } from 'react-router-dom'

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <BrowserRouter>
      <Toaster
        theme="dark"
        richColors
        closeButton
        // m420a/B7 — below the fixed header (h-16, +status banner), never over
        // the nav; bottom-right would hide the mobile composer, so offset instead.
        position="top-center"
        offset={{ top: 88 }}
        mobileOffset={{ top: 88 }}
        duration={2200}
        toastOptions={{
          className: 'vl-neo-toast',
          style: {
            background: '#0b0e11',
            border: '1px solid var(--panel-border)',
            color: 'var(--text-primary)',
          },
        }}
      />
      <App />
    </BrowserRouter>
  </React.StrictMode>
)
