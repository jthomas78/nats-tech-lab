/* Injection key: how a failed plugin route's error panel asks the route to try
   its plugin again. Provided by the component a failed route resolves to (see
   `recoverableRoute` in shellRoutes.js); absent anywhere else. Its own file so
   the panel can import the key without importing the router table. */
export const ROUTE_RETRY = Symbol('route-retry')
