import * as _ from 'lodash';
import { SmartBuffer } from 'smart-buffer';

import { CommandIdentifier, CommandPacket } from '../../src/command/command-contract';
import { CommandBase } from '../../src/command/command.base';
import { MetaCommandBase } from '../../src/command/meta-command.base';
import { CommandAssertion } from './command-assertion';

export abstract class Fixture {
    static stringHexToBuffer(hexStringBuffer: string): Buffer {
        // tester : https://regex101.com/
        const regex = /^[((a-f)|(A-F))0-9]{2}|.b$/;
        const isValidString = hexStringBuffer.match(regex) !== null;
        if (!isValidString) {
            throw new Error(`invalid hex string ${regex}`);
        }
        const buffer = new SmartBuffer();
        while (hexStringBuffer.length >= 2) {
            buffer.writeUInt8(parseInt(hexStringBuffer.substring(0, 2), 16));
            hexStringBuffer = hexStringBuffer.substring(3, hexStringBuffer.length);
        }
        return buffer.toBuffer();
    }

    static buildCharItems(count: number, lengthOfNames: number): string[] {
        const charItems = new Array<string>();
        for (let index = 0; index < count; index++) {
            charItems.push(_.padStart(index.toString(), lengthOfNames, '0'));
        }
        return charItems;
    }

    static assertCommand(
        command: CommandBase<any, any>,
        identifier: CommandIdentifier,
        data: string,
        bytesCount: number,
        checksum: number,
        buffer: string,
        enableLogging = false
    ): void {
        expect(command.id).toBe(identifier.id);
        expect(command.name).toBe(identifier.name);
        expect(command.isExtended).toBe(identifier.isExtended);
        expect(command.rxTxType).toBe(identifier.rxTxType);

        expect(command.startOfMessage).toEqual(CommandPacket.SOM);
        // @TODO: To be updated in the CommandBase
        expect(command.bytesCount).toBe(bytesCount);
        expect(command.checksum).toBe(checksum);
        expect(command.data).toEqual(Fixture.stringHexToBuffer(data));
        expect(command.endOfMessage).toEqual(CommandPacket.EOM);
        expect(command.buffer).toEqual(Fixture.stringHexToBuffer(buffer));

        if (enableLogging) {
            console.log(command.toHexDumpEmulator());
        }
    }

    static assertMetaCommand(
        metaCommand: MetaCommandBase<any, any>,
        identifier: CommandIdentifier,
        metaCommandsAssertion: CommandAssertion[],
        enableLogging = false
    ): void {
        expect(metaCommand.id).toBe(identifier.id);
        expect(metaCommand.name).toBe(identifier.name);
        expect(metaCommand.isExtended).toBe(identifier.isExtended);
        expect(metaCommand.rxTxType).toBe(identifier.rxTxType);

        for (let index = 0; index < metaCommand.commands.length; index++) {
            const command = metaCommand.commands[index];
            const { data, bytesCount, checksum, buffer } = metaCommandsAssertion[index];
            Fixture.assertCommand(command, identifier, data, bytesCount, checksum, buffer, enableLogging);
        }
    }
}
