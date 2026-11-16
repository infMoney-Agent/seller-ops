'use client';

import { useState } from 'react';
import { request, useSellerSession } from '@/features/auth/session';

export function OrgSwitcher() {
  const session = useSellerSession();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  async function change(workspaceId: number) {
    setBusy(true);
    setError('');
    try {
      await request('/api/auth/workspace', 'POST', { workspaceId });
      window.location.replace('/dashboard/workspaces');
    } catch (error) {
      setError(String(error));
      setBusy(false);
    }
  }

  return (
    <div className='space-y-1 px-2'>
      <select
        className='w-full rounded border p-2'
        aria-label='工作区'
        value={session.workspaceId || ''}
        disabled={busy || !session.workspaces.length}
        onChange={(event) => void change(Number(event.target.value))}
      >
        <option value='' disabled>
          选择工作区
        </option>
        {session.workspaces.map((workspace) => (
          <option key={workspace.id} value={workspace.id}>
            {workspace.name}
          </option>
        ))}
      </select>
      {error && <p role='alert'>{error}</p>}
    </div>
  );
}
