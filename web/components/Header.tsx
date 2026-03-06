'use client';

import Link from 'next/link';
import { usePathname } from 'next/navigation';

export function Header() {
  const pathname = usePathname();

  return (
    <header className="header">
      <nav className="nav">
        <Link href="/" className="brand">
          Volga
        </Link>
        <div className="tabs">
          <Link
            href="/"
            className={`tab ${pathname === '/' ? 'tab-active' : ''}`}
          >
            Homepage
          </Link>
          <Link
            href="/saved"
            className={`tab ${pathname === '/saved' ? 'tab-active' : ''}`}
          >
            Saved
          </Link>
          <Link
            href="/record"
            className={`tab ${pathname === '/record' ? 'tab-active' : ''}`}
          >
            Record
          </Link>
        </div>
        <a href="#">Report</a>
        <a href="#">FAQ</a>
      </nav>
      {pathname === '/' && (
        <div className="header-controls">
          <span className="timer">00:00:00</span>
          <span className="timer">00:00:00</span>
          <button type="button" className="btn-secondary">
            Design Preview
          </button>
          <span className="hits">HITs on Page: 1</span>
          <button type="button" className="btn-secondary">
            Debug
          </button>
          <button type="button" className="btn-secondary">
            Exit
          </button>
          <span className="icon">🔔</span>
          <span className="icon">👤</span>
        </div>
      )}
    </header>
  );
}
