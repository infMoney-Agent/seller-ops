'use client';

import { usePathname } from 'next/navigation';
import { useMemo } from 'react';

type BreadcrumbItem = {
  title: string;
  link: string;
};

const routeLabels: Record<string, string> = {
  '/dashboard': '运营总览',
  '/dashboard/ecom': '电商运营',
  '/dashboard/ecom/research': '选品研究',
  '/dashboard/ecom/listing': '商品页面生成',
  '/dashboard/ecom/reviews': '评论分析',
  '/dashboard/ecom/monitor': '竞品监控',
  '/dashboard/ecom/ads': '广告优化',
  '/dashboard/workspaces': '工作区',
  '/dashboard/workspaces/team': '团队管理',
  '/dashboard/profile': '个人资料',
  '/dashboard/notifications': '通知',
  '/dashboard/users': '工作区成员',
  '/dashboard/product': '商品',
  '/dashboard/chat': '对话',
  '/dashboard/kanban': '看板',
  '/dashboard/overview': '总览'
};

export function useBreadcrumbs() {
  const pathname = usePathname();

  const breadcrumbs = useMemo(() => {
    const segments = pathname.split('/').filter(Boolean);
    return segments.map((segment, index) => {
      const path = `/${segments.slice(0, index + 1).join('/')}`;
      return {
        title: routeLabels[path] ?? decodeURIComponent(segment),
        link: path
      };
    });
  }, [pathname]);

  return breadcrumbs;
}
