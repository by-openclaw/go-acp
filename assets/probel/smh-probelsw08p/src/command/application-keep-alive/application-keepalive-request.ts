import { SmartBuffer } from 'smart-buffer';

import { CommandIdentifiers } from '../command-contract';
import { CommandBase } from '../command.base';

export class ApplicationKeepAliveRequestCommand extends CommandBase<any, any> {
    constructor() {
        super(CommandIdentifiers.TX.APP_KEEPALIVE_REQUEST, {});
    }

    toLogDescription(): string {
        return `${this.name}`;
    }

    protected buildData(): Buffer {
        return new SmartBuffer({ size: 1 }).writeUInt8(this.id).toBuffer();
    }
}
