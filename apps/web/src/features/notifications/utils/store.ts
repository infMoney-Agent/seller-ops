import { create } from 'zustand';
// import { persist } from 'zustand/middleware';
import type { NotificationStatus, NotificationAction } from '@/components/ui/notification-card';

export type Notification = {
  id: string;
  title: string;
  body: string;
  status: NotificationStatus;
  createdAt: string;
  actions?: NotificationAction[];
};

type NotificationState = {
  notifications: Notification[];
  markAsRead: (id: string) => void;
  markAllAsRead: () => void;
  removeNotification: (id: string) => void;
  addNotification: (notification: Omit<Notification, 'status'>) => void;
  unreadCount: () => number;
};

const mockNotifications: Notification[] = [
  {
    id: '1',
    title: '新成员加入团队',
    body: 'Sarah Connor 已加入工程工作区。',
    status: 'unread',
    createdAt: new Date(Date.now() - 1000 * 60 * 5).toISOString(),
    actions: [
      {
        id: 'view',
        label: '查看工作区',
        type: 'redirect',
        style: 'primary'
      }
    ]
  },
  {
    id: '2',
    title: '新增商品',
    body: '商品目录已新增「Dashboard Pro」。',
    status: 'unread',
    createdAt: new Date(Date.now() - 1000 * 60 * 30).toISOString(),
    actions: [
      {
        id: 'view-product',
        label: '查看商品',
        type: 'redirect',
        style: 'primary'
      }
    ]
  },
  {
    id: '3',
    title: '账单周期已更新',
    body: '专业版套餐已续订。下一张账单将在 2026 年 4 月 24 日开具。',
    status: 'unread',
    createdAt: new Date(Date.now() - 1000 * 60 * 60 * 2).toISOString(),
    actions: [
      {
        id: 'billing',
        label: '查看账单',
        type: 'redirect',
        style: 'primary'
      }
    ]
  },
  {
    id: '4',
    title: '有一项任务分配给你',
    body: '看板上的「更新仪表盘分析」任务已分配给你。',
    status: 'read',
    createdAt: new Date(Date.now() - 1000 * 60 * 60 * 24).toISOString(),
    actions: [
      {
        id: 'open',
        label: '打开看板',
        type: 'redirect',
        style: 'primary'
      }
    ]
  },
  {
    id: '5',
    title: 'Alex 发来新消息',
    body: 'Alex：「可以一起确认一下总览仪表盘吗？」',
    status: 'read',
    createdAt: new Date(Date.now() - 1000 * 60 * 60 * 24 * 3).toISOString(),
    actions: [
      {
        id: 'open-chat',
        label: '打开对话',
        type: 'redirect',
        style: 'primary'
      }
    ]
  }
];

export const useNotificationStore = create<NotificationState>()(
  // To enable persistence across refreshes, uncomment the persist wrapper below:
  // persist(
  (set, get) => ({
    notifications: mockNotifications,

    markAsRead: (id) =>
      set((state) => ({
        notifications: state.notifications.map((n) =>
          n.id === id ? { ...n, status: 'read' as const } : n
        )
      })),

    markAllAsRead: () =>
      set((state) => ({
        notifications: state.notifications.map((n) => ({
          ...n,
          status: 'read' as const
        }))
      })),

    removeNotification: (id) =>
      set((state) => ({
        notifications: state.notifications.filter((n) => n.id !== id)
      })),

    addNotification: (notification) =>
      set((state) => ({
        notifications: [{ ...notification, status: 'unread' as const }, ...state.notifications]
      })),

    unreadCount: () => get().notifications.filter((n) => n.status === 'unread').length
  })
  //   ,
  //   { name: 'notifications' }
  // )
);
