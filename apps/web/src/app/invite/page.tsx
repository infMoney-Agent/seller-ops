'use client';

import { useState } from 'react';
import { request } from '@/features/auth/session';

export default function InvitePage() {
  const [message, setMessage] = useState('');
  const [busy, setBusy] = useState(false);
  return (
    <main className='mx-auto max-w-md space-y-4 p-8'>
      <h1 className='text-2xl font-semibold'>工作区邀请</h1>
      <p>请使用受邀邮箱登录，然后重新打开此链接接受邀请。</p>
      <form action='/api/auth/login' method='get'>
        <button className='underline'>登录</button>
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
            window.location.replace('/dashboard');
          } catch (error) {
            setMessage(String(error));
            setBusy(false);
          }
        }}
      >
        接受邀请
      </button>
      {message && <p role='alert'>{message}</p>}
    </main>
  );
}
