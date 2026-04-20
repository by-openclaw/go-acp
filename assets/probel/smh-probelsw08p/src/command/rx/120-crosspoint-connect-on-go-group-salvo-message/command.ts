import { SmartBuffer } from 'smart-buffer';

import { LocaleData } from '../../../common/locale-data/locale-data.model';
import { BufferUtility } from '../../../common/utility/buffer.utility';
import { CommandIdentifiers, ValidationError } from '../../command-contract';
import { LocaleDataCache } from '../../command-locale-data-cache';
import { CommandErrorsKeys } from '../../locale-data-keys';
import { MetaCommandBase } from '../../meta-command.base';
import { CommandItemsUtility, CrossPointConnectOnGoSalvoGroupMessageCommandItems } from './items';
import { CommandParamsUtility, CrossPointConnectOnGoSalvoGroupMessageCommandParams } from './params';
import { CommandParamsValidator } from './params.validator';

/**
 * Implements the CrossPoint Connect On Go Salvo Group Message command
 *
 * This message is issued by the remote device to set up salvo switches.
 * Routing information is held in the receiving device until activated by CROSSPOINT GO GROUP SALVO command (Command Byte 121).
 * The controller will respond with a CROSSPOINT CONNECT ON GO GROUP SALVO ACKNOWLEDGE message (Command Byte 122) to indicate that the routing information has been stored successfully.
 * N.B. : The group salvo commands are only implemented on the XD and ECLIPSE router ranges.
 *
 * Command issued by the remote device
 * @export
 * @class CrossPointConnectOnGoSalvoGroupMessageCommand
 * @extends {MetaCommandBase<CrossPointConnectOnGoSalvoGroupMessageCommandParams>}
 */
export class CrossPointConnectOnGoSalvoGroupMessageCommand extends MetaCommandBase<
    CrossPointConnectOnGoSalvoGroupMessageCommandParams,
    any
> {
    /**
     * Creates an instance of CrossPointConnectOnGoSalvoGroupMessageCommand.
     * @param {CrossPointConnectOnGoSalvoGroupMessageCommandParams} params the command parameters
     * @memberof CrossPointConnectOnGoSalvoGroupMessageCommand
     */
    constructor(params: CrossPointConnectOnGoSalvoGroupMessageCommandParams) {
        super(CommandIdentifiers.RX.GENERAL.CROSSPOINT_CONNECT_ON_GO_GROUP_SALVO_MESSAGE, params);
        this.validateParams(params);
    }

    /**
     * Gets a textual representation of the command including params (mainly used for logging purpose)
     *
     * @returns {string} the command textual description
     * @memberof CrossPointConnectOnGoSalvoGroupMessageCommand
     */
    toLogDescription(): string {
        const descriptionFor = (type: string): string =>
            `${type} - ${this.name}: ${CommandParamsUtility.toString(this.params)} ${CommandItemsUtility.toString(
                this.params.salvoGroupMessageCommandItems[0]
            )}`;

        return descriptionFor('General');
    }

    /**
     * Builds the commands
     *
     * @protected
     * @returns {Buffer[]} the command message
     * @memberof CrossPointConnectOnGoSalvoGroupMessageCommand
     */
    protected buildData(): Buffer[] {
        // Gets the list of command to send
        const buildDataArray = new Array<Buffer>();

        // Generate the List of Command to send
        for (let byteIndex = 0; byteIndex < this.params.salvoGroupMessageCommandItems.length; byteIndex++) {
            const data = this.params.salvoGroupMessageCommandItems[byteIndex];
            if (data.matrixId > 15 || data.levelId > 15 || data.destinationId > 895 || data.sourceId > 1023) {
                // build Extended Command & Add the command buffer to the array
                buildDataArray.push(this.buildDataExtended(data));
            } else {
                // build General Command & Add the command buffer to the array
                buildDataArray.push(this.buildDataNormal(data));
            }
        }
        return buildDataArray;
    }

    /**
     * Validates the command parameters, options and throws a ValidationError in error(s) occur
     *
     * @private
     * @param {CrossPointConnectOnGoSalvoGroupMessageCommandParams} params the command parameters
     * @memberof CrossPointConnectOnGoSalvoGroupMessageCommand
     */
    private validateParams(params: CrossPointConnectOnGoSalvoGroupMessageCommandParams): void {
        const validationErrors: Record<string, LocaleData> = new CommandParamsValidator(params).validate();

        if (Object.keys(validationErrors).length > 0) {
            const localeData = LocaleDataCache.INSTANCE.getCommandErrorLocaleData(
                CommandErrorsKeys.CREATE_COMMAND_FAILURE
            );
            throw new ValidationError(localeData.description, validationErrors);
        }
    }

    /**
     * Builds general command.
     * Returns Probel SW-P-08 - General Crosspoint Connect On Go Group Salvo Message CMD_120_0X78
     *
     * + This message is issued by the remote device to set up salvo switches.
     * + Routing information is held in the receiving device until activated by CROSSPOINT GO GROUP SALVO command (Command Byte 121).
     * + The controller will respond with a CROSSPOINT CONNECT ON GO GROUP SALVO ACKNOWLEDGE message (Command Byte 122) to indicate that the routing information has been stored successfully.
     *
     * + N.B.: The group salvo commands are only implemented on the XD and ECLIPSE routerranges.
     *
     * | Message | Command Byte | 120 - 0x78                                                                                                                         |
     * |---------|--------------|------------------------------------------------------------------------------------------------------------------------------------|
     * | Byte    | Field Format | Notes                                                                                                                              |
     * | Byte 1  |              | Matrix/Level number                                                                                                                |
     * |         | Bits[4-7]    | Matrix Number                                                                                                                      |
     * |         | Bits[0-3]    | Level Number                                                                                                                       |
     * | Byte 2  | Multiplier   | this field allows sources and dests of up to 1023 to be used and provides source status info from a TDM or HD Digital Video router |
     * |         | Bit[7]       | 0                                                                                                                                  |
     * |         | Bits[4-6]    | Dest number DIV 128                                                                                                                |
     * |         | Bit[3]       | TDM and Digital Video source "bad" status (0 = good source)                                                                        |
     * |         | Bits[0-2]    | Source number DIV 128                                                                                                              |
     * | Byte 3  | Dest number  | Destination number MOD 128                                                                                                         |
     * | Byte 4  | Src  number  | Source number MOD 128                                                                                                              |
     * | Byte 5  | Salvo number | Salvo number MOD 128                                                                                                               |
     *
     * @private
     * @param {CrossPointConnectOnGoSalvoGroupMessageCommandItems} items the command parameters
     * @returns {Buffer} the command message
     * @memberof CrossPointConnectOnGoSalvoGroupMessageCommand
     */
    private buildDataNormal(items: CrossPointConnectOnGoSalvoGroupMessageCommandItems): Buffer {
        return new SmartBuffer({ size: 6 })
            .writeUInt8(CommandIdentifiers.RX.GENERAL.CROSSPOINT_CONNECT_ON_GO_GROUP_SALVO_MESSAGE.id)
            .writeUInt8(BufferUtility.combine2BytesMsbLsb(items.matrixId, items.levelId))
            .writeUInt8(BufferUtility.combine2BytesMultiplierMsbLsb(items.destinationId, items.sourceId))
            .writeUInt8(items.destinationId % 128)
            .writeUInt8(items.sourceId % 128)
            .writeUInt8(items.salvoId % 128)
            .toBuffer();
    }

    /**
     * Builds extended command
     * + Returns Probel SW-P-08 - Extended General CrossPoint Connect Message CMD_248_0Xf8
     *
     * + This message is issued by the remote device to set up salvo switches.
     * + Routing information is held in the receiving device until activated by CROSSPOINT GO GROUP SALVO command (Command Byte 121).
     * + The controller will respond with an EXTENDED CROSSPOINT CONNECT ON GO GROUP SALVO  ACKNOWLEDGE message (Command Byte 250) to indicate that the routing information has been stored successfully.
     *
     * + N.B.	The group salvo commands are only implemented on the XD and ECLIPSE router ranges.
     *
     * | Message |  Command Byte   | 248 - 0xf8                                                                                                                      |
     * |---------|-----------------|---------------------------------------------------------------------------------------------------------------------------------|
     * | Byte    | Field Format    | Notes                                                                                                                           |
     * | Byte 1  | Matrix number   |                                                                                                                                 |
     * | Byte 2  | Level number    |                                                                                                                                 |
     * | Byte 3  | Dest multiplier | Destination number DIV 256                                                                                                      |
     * | Byte 4  | Dest number     | Destination number MOD 256                                                                                                      |
     * | Byte 5  | Src multiplier  | Source number DIV 256                                                                                                           |
     * | Byte 6  | Src number      | Source number MOD 256                                                                                                           |
     * | Byte 7  | Salvo num       | Holds the Salvo group number to configure                                                                                       |
     * |         | Bit[7]          | 0                                                                                                                               |
     * |         | Bit[0-6]        | Salvo number 0-127                                                                                                              |
     * |         |                 | Destination and source will always overwrite previous data.                                                                     |
     *
     * @private
     * @param {CrossPointConnectOnGoSalvoGroupMessageCommandItems} items
     * @returns {Buffer}
     * @memberof CrossPointConnectOnGoSalvoGroupMessageCommand
     */
    private buildDataExtended(items: CrossPointConnectOnGoSalvoGroupMessageCommandItems): Buffer {
        const buffer = new SmartBuffer({ size: 8 })
            .writeUInt8(CommandIdentifiers.RX.EXTENDED.CROSSPOINT_CONNECT_ON_GO_GROUP_SALVO_MESSAGE.id)
            .writeUInt8(items.matrixId)
            .writeUInt8(items.levelId)
            .writeUInt8(Math.floor(items.destinationId / 256))
            .writeUInt8(items.destinationId % 256)
            .writeUInt8(Math.floor(items.sourceId / 256))
            .writeUInt8(items.sourceId % 256)
            .writeUInt8(items.salvoId);
        return buffer.toBuffer();
    }
}
