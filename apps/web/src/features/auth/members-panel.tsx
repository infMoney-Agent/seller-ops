'use client';

import { useState } from 'react';
import { request, type Role } from './session';

export type Member = { id: number; name: string; email: string; role: Role };
export type Invitation = { id: string; email: string; expiresAt: string };

export function MembersPanel({
  members,
  invitations,
  owner
}: {
  members: Member[];
  invitations: Invitation[];
  owner: boolean;
}) {
  const [currentMembers, setMembers] = useState(members);
  const [currentInvites, setInvites] = useState(invitations);
  const [email, setEmail] = useState('');
  const [link, setLink] = useState('');
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);

  async function mutate(action: () => Promise<unknown>) {
    setBusy(true);
    setError('');
    try {
      await action();
      const [members, invites] = await Promise.all([
        request<Member[]>('/api/users'),
        request<Invitation[]>('/api/invitations')
      ]);
      setMembers(members);
      setInvites(invites);
    } catch (error) {
      setError(String(error));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className='space-y-4'>
      {error && <p role='alert'>{error}</p>}
      <ul className='space-y-2'>
        {currentMembers.map((member) => (
          <li key={member.id} className='flex flex-wrap items-center gap-3'>
            <span>
              {member.name} · {member.email}
            </span>
            {owner ? (
              <>
                <select
                  aria-label={`成员角色： ${member.email}`}
                  disabled={busy}
                  value={member.role}
                  onChange={(event) => {
                    const role = event.target.value;
                    void mutate(() => request(`/api/users/${member.id}`, 'PATCH', { role }));
                  }}
                >
                  <option value='owner'>所有者</option>
                  <option value='member'>成员</option>
                </select>
                <button
                  type='button'
                  className='underline'
                  disabled={busy}
                  onClick={() => {
                    if (window.confirm(`移除 ${member.email} 从此工作区移除吗？`)) {
                      void mutate(() => request(`/api/users/${member.id}`, 'DELETE'));
                    }
                  }}
                >
                  移除
                </button>
              </>
            ) : (
              <span>{member.role}</span>
            )}
          </li>
        ))}
      </ul>
      {owner && (
        <>
          <form
            className='flex gap-2'
            onSubmit={(event) => {
              event.preventDefault();
              setLink('');
              void mutate(async () => {
                const data = await request<{ url: string }>('/api/invitations', 'POST', { email });
                setLink(data.url);
                setEmail('');
              });
            }}
          >
            <input
              className='rounded border p-2'
              aria-label='已验证的邮箱'
              required
              type='email'
              value={email}
              onChange={(event) => setEmail(event.target.value)}
            />
            <button className='underline' disabled={busy}>
              创建邀请
            </button>
          </form>
          {link && <p className='break-all'>请将此链接分享给受邀成员： {link}</p>}
          <ul className='space-y-2'>
            {currentInvites.map((invite) => (
              <li key={invite.id} className='flex gap-3'>
                <span>
                  {invite.email} · 到期时间 {invite.expiresAt.slice(0, 10)}
                </span>
                <button
                  type='button'
                  className='underline'
                  disabled={busy}
                  onClick={() =>
                    void mutate(() => request(`/api/invitations/${invite.id}`, 'DELETE'))
                  }
                >
                  撤销
                </button>
              </li>
            ))}
          </ul>
        </>
      )}
    </div>
  );
}
