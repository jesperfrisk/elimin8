import React, { useEffect, useState } from 'react';
import { createRoot } from 'react-dom/client';
import './style.css';

function App() {
  const [status, setStatus] = useState('Connecting…');

  useEffect(() => {
    const controller = new AbortController();
    const timeout = setTimeout(() => controller.abort(), 5000);

    async function checkConnection() {
      try {
        const response = await fetch('/api/health/ready', { signal: controller.signal });
        if (!response.ok) throw new Error('API unavailable');
        const health = await response.json();
        setStatus(health.status === 'ok' ? 'Connected' : 'Unavailable');
      } catch {
        setStatus('Unavailable — try refreshing in a moment.');
      } finally {
        clearTimeout(timeout);
      }
    }

    checkConnection();
    return () => {
      clearTimeout(timeout);
      controller.abort();
    };
  }, []);

  return (
    <main>
      <p className="eyebrow">Everyone is a target.</p>
      <h1>Elimin8</h1>
      <p className="intro">Trust no one. Complete your mission. Be the last one standing.</p>
      <p>The game is taking shape. Check back soon.</p>
      <p className="status" role="status">Connection: {status}</p>
    </main>
  );
}

createRoot(document.getElementById('root')).render(<App />);
