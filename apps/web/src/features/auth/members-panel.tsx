'use client';

import { useState } from 'react';
import { request, type Role } from './session';

export type Member = { id: number; name: string; email: string; role: Role };
export type Invitation = { id: string; email: string; expiresAt: string };
export type JoinRequest = { id: number; name: string; email: string; createdAt: string };
export type CompanyInfo = { name: string; code: string };

export function MembersPanel({
  members,
  invitations,
  owner,
  company,
  joinRequests = []
}: {
  members: Member[];
  invitations: Invitation[];
  owner: boolean;
  company: CompanyInfo;
  joinRequests?: JoinRequest[];
}) {
  const [currentMembers, setMembers] = useState(members);
  const [currentInvites, setInvites] = useState(invitations);
  const [email, setEmail] = useState('');
  const [link, setLink] = useState('');
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const [pendingRequests, setPendingRequests] = useState(joinRequests);

  async function mutate(action: () => Promise<unknown>) {
    setBusy(true);
    setError('');
    try {
      await action();
      const [members, invites, requests] = await Promise.all([
        request<Member[]>('/api/users'),
        request<Invitation[]>('/api/invitations'),
        request<JoinRequest[]>('/api/auth/join-requests')
      ]);
      setMembers(members);
      setInvites(invites);
      setPendingRequests(requests);
    } catch (error) {
      setError(String(error));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className='space-y-4'>
      <div className='rounded-lg border p-4'>
        <p className='font-semibold'>{company.name}</p>
        <p className='text-sm text-muted-foreground'>
          公司编号：<span className='select-all font-mono'>{company.code}</span>
        </p>
      </div>
      {error && <p role='alert'>{error}</p>}
      {owner && (
        <section
          className='space-y-3 rounded-lg border p-4'
          aria-labelledby='join-requests-heading'
        >
          <h2 id='join-requests-heading' className='font-semibold'>
            加入申请（{pendingRequests.length}）
          </h2>
          <div aria-live='polite'>
            {pendingRequests.length === 0 && (
              <p className='text-sm text-muted-foreground'>暂无待审批申请</p>
            )}
            <ul className='divide-y'>
              {pendingRequests.map((item) => (
                <li
                  key={item.id}
                  className='flex flex-wrap items-center justify-between gap-3 py-3'
                >
                  <div>
                    <p>
                      {item.name} · {item.email}
                    </p>
                    <p className='text-xs text-muted-foreground'>
                      {new Date(item.createdAt).toLocaleString('zh-CN')}
                    </p>
                  </div>
                  <div className='flex gap-2'>
                    <button
                      type='button'
                      className='min-h-11 rounded-md border px-4 focus-visible:outline-2'
                      disabled={busy}
                      aria-label={`批准 ${item.name} 加入公司`}
                      onClick={() =>
                        void mutate(() =>
                          request(`/api/auth/join-requests/${item.id}/approve`, 'POST', {})
                        )
                      }
                    >
                      批准
                    </button>
                    <button
                      type='button'
                      className='min-h-11 rounded-md border px-4 text-red-600 focus-visible:outline-2'
                      disabled={busy}
                      aria-label={`拒绝 ${item.name} 加入公司`}
                      onClick={() =>
                        void mutate(() =>
                          request(`/api/auth/join-requests/${item.id}/reject`, 'POST', {})
                        )
                      }
                    >
                      拒绝
                    </button>
                  </div>
                </li>
              ))}
            </ul>
          </div>
        </section>
      )}
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
              aria-label='同事邮箱'
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
