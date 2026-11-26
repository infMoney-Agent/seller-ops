'use client';
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger
} from '@/components/ui/dropdown-menu';
import {
  Sidebar,
  SidebarContent,
  SidebarHeader,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupLabel,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarMenuSub,
  SidebarMenuSubButton,
  SidebarMenuSubItem,
  SidebarRail
} from '@/components/ui/sidebar';
import { UserAvatarProfile } from '@/components/user-avatar-profile';
import { navGroups } from '@/config/nav-config';
import { signOut, useSellerSession, useSellerUser } from '@/features/auth/session';
import { useScreeningQuery } from '@/features/screening/queries';
import { useFilteredNavGroups } from '@/hooks/use-nav';
import Link from 'next/link';
import { usePathname, useRouter } from 'next/navigation';
import { Icons } from '../icons';

function ScreeningPendingIndicator() {
  const session = useSellerSession();
  const counts = useScreeningQuery<Record<string, number>>(
    session.workspaceId > 0 ? '/summary?' : null
  );
  const pending = counts.data?.pending_displayed ?? 0;
  if (pending <= 0) return null;
  return (
    <span
      title={`有 ${pending.toLocaleString('zh-CN')} 项待人工复核`}
      className='ml-auto inline-flex items-center gap-1 rounded-full bg-amber-500/15 px-1.5 py-0.5 font-mono text-[11px] font-medium tabular-nums text-amber-700 group-data-[collapsible=icon]:hidden dark:text-amber-300'
    >
      <span aria-hidden='true' className='size-1.5 rounded-full bg-amber-500' />
      <span>{pending > 999 ? '999+' : pending.toLocaleString('zh-CN')}</span>
    </span>
  );
}

export default function AppSidebar() {
  const pathname = usePathname();
  const user = useSellerUser();
  const router = useRouter();
  const filteredGroups = useFilteredNavGroups(navGroups);

  return (
    <Sidebar collapsible='icon'>
      <SidebarHeader className='border-b px-3 py-4'>
        <Link
          href='/dashboard'
          aria-label='Seller Ops 电商运营工作台'
          className='flex items-center gap-2.5 group-data-[collapsible=icon]:justify-center'
        >
          <span className='bg-primary text-primary-foreground flex size-8 shrink-0 items-center justify-center rounded-lg text-xs font-bold'>
            SO
          </span>
          <div className='min-w-0 group-data-[collapsible=icon]:hidden'>
            <p className='text-sm font-semibold tracking-tight'>Seller Ops</p>
            <p className='text-muted-foreground text-xs'>电商运营工作台</p>
          </div>
        </Link>
      </SidebarHeader>
      <SidebarContent className='overflow-x-hidden'>
        {filteredGroups.map((group) => (
          <SidebarGroup key={group.label || 'ungrouped'} className='py-0'>
            {group.label && (
              <SidebarGroupLabel className='group-data-[collapsible=icon]:hidden'>
                {group.label}
              </SidebarGroupLabel>
            )}
            <SidebarMenu>
              {group.items.map((item) => {
                const Icon = item.icon ? Icons[item.icon] : Icons.logo;
                return item?.items && item?.items?.length > 0 ? (
                  <Collapsible
                    key={item.title}
                    defaultOpen={item.isActive}
                    render={<SidebarMenuItem />}
                  >
                    <CollapsibleTrigger
                      render={
                        <SidebarMenuButton
                          tooltip={item.title}
                          isActive={pathname === item.url}
                          className='group/collapsible'
                        />
                      }
                    >
                      {item.icon && <Icon />}
                      <span>{item.title}</span>
                      <Icons.chevronRight className='ml-auto transition-transform duration-200 group-data-panel-open/collapsible:rotate-90' />
                    </CollapsibleTrigger>
                    <CollapsibleContent>
                      <SidebarMenuSub>
                        {item.items?.map((subItem) => (
                          <SidebarMenuSubItem key={subItem.title}>
                            <SidebarMenuSubButton
                              render={<Link href={subItem.url} aria-label={subItem.title} />}
                              isActive={pathname === subItem.url}
                            >
                              <span>{subItem.title}</span>
                            </SidebarMenuSubButton>
                          </SidebarMenuSubItem>
                        ))}
                      </SidebarMenuSub>
                    </CollapsibleContent>
                  </Collapsible>
                ) : (
                  <SidebarMenuItem key={item.title}>
                    <SidebarMenuButton
                      render={<Link href={item.url} aria-label={item.title} />}
                      tooltip={item.title}
                      isActive={pathname === item.url}
                    >
                      <Icon />
                      <span>{item.title}</span>
                      {item.url === '/dashboard/ecom/screening' && <ScreeningPendingIndicator />}
                    </SidebarMenuButton>
                  </SidebarMenuItem>
                );
              })}
            </SidebarMenu>
          </SidebarGroup>
        ))}
      </SidebarContent>
      <SidebarFooter>
        <SidebarMenu>
          <SidebarMenuItem>
            <DropdownMenu>
              <DropdownMenuTrigger
                render={
                  <SidebarMenuButton
                    size='lg'
                    className='data-popup-open:bg-sidebar-accent data-popup-open:text-sidebar-accent-foreground'
                  />
                }
              >
                {user && <UserAvatarProfile className='h-8 w-8 rounded-lg' showInfo user={user} />}
                <Icons.chevronsDown className='ml-auto size-4' />
              </DropdownMenuTrigger>
              <DropdownMenuContent
                className='w-(--anchor-width) min-w-56 rounded-lg'
                side='bottom'
                align='end'
                sideOffset={4}
              >
                <DropdownMenuGroup>
                  <DropdownMenuLabel className='p-0 font-normal'>
                    <div className='px-1 py-1.5'>
                      {user && (
                        <UserAvatarProfile className='h-8 w-8 rounded-lg' showInfo user={user} />
                      )}
                    </div>
                  </DropdownMenuLabel>
                </DropdownMenuGroup>
                <DropdownMenuSeparator />

                <DropdownMenuGroup>
                  <DropdownMenuItem onClick={() => router.push('/dashboard/profile')}>
                    <Icons.account className='mr-2 h-4 w-4' />
                    个人资料
                  </DropdownMenuItem>
                  <DropdownMenuItem onClick={() => router.push('/dashboard/notifications')}>
                    <Icons.notification className='mr-2 h-4 w-4' />
                    通知
                  </DropdownMenuItem>
                </DropdownMenuGroup>
                <DropdownMenuSeparator />
                <DropdownMenuGroup>
                  <DropdownMenuItem onClick={() => signOut()}>
                    <Icons.logout aria-hidden className='mr-2 h-4 w-4' />
                    退出登录
                  </DropdownMenuItem>
                </DropdownMenuGroup>
              </DropdownMenuContent>
            </DropdownMenu>
          </SidebarMenuItem>
        </SidebarMenu>
      </SidebarFooter>
      <SidebarRail />
    </Sidebar>
  );
}
