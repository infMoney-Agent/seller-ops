export const authRPCRoutes = [
  {
    name: 'GetSession',
    method: 'GET',
    path: '/api/auth/session',
    body: null,
    response: 'SessionProfile',
    usecase: 'session',
    owner: false,
    array: false
  },
  {
    name: 'ListManagedProducts',
    method: 'GET',
    path: '/api/products',
    body: null,
    response: 'ProductListResult',
    usecase: 'products',
    owner: false,
    array: false
  },
  {
    name: 'GetManagedProduct',
    method: 'GET',
    path: '/api/products/{id}',
    body: null,
    response: 'ProductResult',
    usecase: 'products',
    owner: false,
    array: false
  },
  {
    name: 'CreateManagedProduct',
    method: 'POST',
    path: '/api/products',
    body: 'ManagedProductInput',
    response: 'ProductResult',
    usecase: 'products',
    owner: true,
    array: false
  },
  {
    name: 'UpdateManagedProduct',
    method: 'PUT',
    path: '/api/products/{id}',
    body: 'ManagedProductInput',
    response: 'ProductResult',
    usecase: 'products',
    owner: true,
    array: false
  },
  {
    name: 'PatchManagedProduct',
    method: 'PATCH',
    path: '/api/products/{id}',
    body: 'ManagedProductInput',
    response: 'ProductResult',
    usecase: 'products',
    owner: true,
    array: false
  },
  {
    name: 'DeleteManagedProduct',
    method: 'DELETE',
    path: '/api/products/{id}',
    body: null,
    response: 'ProductResult',
    usecase: 'products',
    owner: true,
    array: false
  },
  {
    name: 'ListMembers',
    method: 'GET',
    path: '/api/users',
    body: null,
    response: 'MemberItems',
    usecase: 'members',
    owner: false,
    array: true
  },
  {
    name: 'UpdateMember',
    method: 'PATCH',
    path: '/api/users/{id}',
    body: 'MemberInput',
    response: 'google.protobuf.Empty',
    usecase: 'changeMember',
    owner: true,
    array: false
  },
  {
    name: 'RemoveMember',
    method: 'DELETE',
    path: '/api/users/{id}',
    body: null,
    response: 'google.protobuf.Empty',
    usecase: 'changeMember',
    owner: true,
    array: false
  },
  {
    name: 'ListInvitations',
    method: 'GET',
    path: '/api/invitations',
    body: null,
    response: 'InvitationItems',
    usecase: 'invitations',
    owner: true,
    array: true
  },
  {
    name: 'CreateInvitation',
    method: 'POST',
    path: '/api/invitations',
    body: 'InvitationInput',
    response: 'InvitationURL',
    usecase: 'invitations',
    owner: true,
    array: false
  },
  {
    name: 'RevokeInvitation',
    method: 'DELETE',
    path: '/api/invitations/{id}',
    body: null,
    response: 'google.protobuf.Empty',
    usecase: 'invitations',
    owner: true,
    array: false
  },
  {
    name: 'InspectInvitation',
    method: 'POST',
    path: '/api/invitations/inspect',
    body: 'InvitationToken',
    response: 'InvitationInspection',
    usecase: 'inspectInvitation',
    owner: false,
    array: false
  },
  {
    name: 'AcceptInvitation',
    method: 'POST',
    path: '/api/invitations/accept',
    body: 'InvitationToken',
    response: 'WorkspaceJoined',
    usecase: 'accept',
    owner: false,
    array: false
  },
  {
    name: 'GetCompany',
    method: 'GET',
    path: '/api/auth/company',
    body: null,
    response: 'Company',
    usecase: 'CompanyInfo',
    owner: false,
    array: false
  },
  {
    name: 'GetRegistrationStatus',
    method: 'GET',
    path: '/api/auth/registration/status',
    body: null,
    response: 'RegistrationStatus',
    usecase: 'RegistrationStatus',
    owner: false,
    array: false
  },
  {
    name: 'ListJoinRequests',
    method: 'GET',
    path: '/api/auth/join-requests',
    body: null,
    response: 'JoinApplicants',
    usecase: 'JoinRequests',
    owner: false,
    array: true
  },
  {
    name: 'ApplyToCompany',
    method: 'POST',
    path: '/api/auth/join-requests',
    body: 'CompanyTarget',
    response: 'ApplyCompanyResult',
    usecase: 'ApplyToCompany',
    owner: false,
    array: false
  },
  {
    name: 'DecideJoinRequest',
    method: 'POST',
    path: '/api/auth/join-requests/{id}/{action}',
    body: null,
    response: 'JoinDecisionResult',
    usecase: 'DecideJoinRequest',
    owner: false,
    array: false
  }
] as const;
