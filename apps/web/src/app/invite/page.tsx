'use client';

import { useState } from 'react';
import { request } from '@/features/auth/session';

export default function InvitePage() {
  const [message, setMessage] = useState('');
  const [busy, setBusy] = useState(false);
  return (
    <main className='mx-auto max-w-md space-y-4 p-8'>
      <h1 className='text-2xl font-semibold'>Workspace invitation</h1>
      <p>Sign in with the invited email. Then reopen this invitation link to accept it.</p>
      <form action='/api/auth/login' method='get'>
        <button className='underline'>Sign in</button>
      </form>
      <button
        type='button'
        className='block underline'
        disabled={busy}
        onClick={async () => {
          setBusy(true);
          try {
            await request('/api/invitations/accept', 'POST', {
              token: window.location.hash.slice(1)
            });
            window.location.replace('/dashboard/workspaces');
          } catch (error) {
            setMessage(String(error));
            setBusy(false);
          }
        }}
      >
        Accept invitation
      </button>
      {message && <p role='alert'>{message}</p>}
    </main>
  );
}
