import type { HttpClient } from '../http.js';
import type { CreateUserInput, User, UsersApi } from '../types.js';

export class UsersResource implements UsersApi {
  constructor(private readonly http: HttpClient) {}

  create(input: CreateUserInput): Promise<User> {
    return this.http.post<User>('/api/v1/users', input);
  }
}
