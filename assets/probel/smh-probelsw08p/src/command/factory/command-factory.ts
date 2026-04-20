import { CommandBase } from '../command.base';
import { BufferredCommand } from './buffered-command';

export class CommandFactory {
    static fromParams<TParams, TOptions, TCommand = CommandBase<TParams, TOptions>>(
        params: TParams,
        options: TOptions,
        type: new (params: TParams, options: TOptions) => TCommand
    ): TCommand {
        const command: TCommand = new type(params, options);
        (<any>command).buildCommand(); // Should be encode
        return command;
    }

    static fromBuffer(buffer: Buffer): CommandBase<any, any> {
        const command = new BufferredCommand();
        command.decode(buffer);
        return command;
    }
}
