import { StrictMode, Suspense } from 'react'
import { createRoot } from 'react-dom/client'
import App from './App'
import { bootstrapApplication } from './bootstrap'
import './index.css'
import { initI18n } from './i18n'

const root = document.getElementById('root')
if (!root) {
  throw new Error('Merlon root element was not found')
}

void bootstrapApplication({
  initialize: initI18n,
  render: () => {
    createRoot(root).render(
      <StrictMode>
        <Suspense fallback={null}>
          <App />
        </Suspense>
      </StrictMode>,
    )
  },
  reportError: (error) => {
    console.error('Failed to initialize Merlon translations', error)
  },
})
