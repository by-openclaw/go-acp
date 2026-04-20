import { CrossPointConnectOnGoGroupSalvoAcknowledgeMessageCommand } from '../../../../src/command/tx/122-crosspoint-connect-on-go-group-salvo-message/command';
import { CrossPointConnectOnGoGroupSalvoAcknowledgeMessageCommandParams } from '../../../../src/command/tx/122-crosspoint-connect-on-go-group-salvo-message/params';
import { Fixture } from '../../../fixture/fixture';
import { BootstrapService } from '../../../../src/bootstrap.service';
import { ValidationError, CommandIdentifiers, CommandIdentifier } from '../../../../src/command/command-contract';
import { CommandErrorsKeys } from '../../../../src/command/locale-data-keys';

describe('CrossPoint Connect On Go Group Salvo Acknowledge Message', () => {
    beforeAll(async () => {
        await BootstrapService.bootstrapAsync('en');
    });

    describe('ctor', () => {
        it('Should instantiate the command with valid params', () => {
            // Arrange
            const params: CrossPointConnectOnGoGroupSalvoAcknowledgeMessageCommandParams = {
                matrixId: 0,
                levelId: 0,
                destinationId: 0,
                sourceId: 0,
                salvoId: 0
            };

            // Act
            const command = new CrossPointConnectOnGoGroupSalvoAcknowledgeMessageCommand(params);

            // Assert
            expect(command).toBeDefined();
        });

        it('Should throw an error if params params are out of range < MIN', done => {
            // Arrange
            const params: CrossPointConnectOnGoGroupSalvoAcknowledgeMessageCommandParams = {
                matrixId: -1,
                levelId: -1,
                destinationId: -1,
                sourceId: -1,
                salvoId: -1
            };

            // Act
            const fct = () => new CrossPointConnectOnGoGroupSalvoAcknowledgeMessageCommand(params);

            // Assert
            try {
                fct();
            } catch (e) {
                expect(e).toBeInstanceOf(ValidationError);
                const localeDataError = e as ValidationError;
                expect(localeDataError.validationErrors).toBeDefined();
                expect(localeDataError.message).toBeDefined();

                expect(localeDataError.validationErrors?.matrixId.id).toBe(
                    CommandErrorsKeys.MATRIXID_IS_OUT_OF_RANGE_ERROR_MSG
                );
                expect(localeDataError.validationErrors?.levelId.id).toBe(
                    CommandErrorsKeys.LEVEL_IS_OUT_OF_RANGE_ERROR_MSG
                );
                expect(localeDataError.validationErrors?.destinationId.id).toBe(
                    CommandErrorsKeys.DESTINATION_IS_OUT_OF_RANGE_ERROR_MSG
                );
                expect(localeDataError.validationErrors?.sourceId.id).toBe(
                    CommandErrorsKeys.SOURCE_IS_OUT_OF_RANGE_ERROR_MSG
                );
                expect(localeDataError.validationErrors?.salvoId.id).toBe(
                    CommandErrorsKeys.SALVO_IS_OUT_OF_RANGE_ERROR_MSG
                );
                done();
            }
        });

        it('Should throw an error if params params are out of range > MAX', done => {
            // Arrange
            const params: CrossPointConnectOnGoGroupSalvoAcknowledgeMessageCommandParams = {
                matrixId: 256,
                levelId: 256,
                destinationId: 65536,
                sourceId: 65536,
                salvoId: 128
            };

            // Act
            const fct = () => new CrossPointConnectOnGoGroupSalvoAcknowledgeMessageCommand(params);

            // Assert
            try {
                fct();
            } catch (e) {
                expect(e).toBeInstanceOf(ValidationError);
                const localeDataError = e as ValidationError;
                expect(localeDataError.validationErrors).toBeDefined();
                expect(localeDataError.message).toBeDefined();
                expect(localeDataError.validationErrors?.destinationId.id).toBe(
                    CommandErrorsKeys.DESTINATION_IS_OUT_OF_RANGE_ERROR_MSG
                );
                expect(localeDataError.validationErrors?.matrixId.id).toBe(
                    CommandErrorsKeys.MATRIXID_IS_OUT_OF_RANGE_ERROR_MSG
                );
                expect(localeDataError.validationErrors?.levelId.id).toBe(
                    CommandErrorsKeys.LEVEL_IS_OUT_OF_RANGE_ERROR_MSG
                );
                expect(localeDataError.validationErrors?.sourceId.id).toBe(
                    CommandErrorsKeys.SOURCE_IS_OUT_OF_RANGE_ERROR_MSG
                );
                expect(localeDataError.validationErrors?.salvoId.id).toBe(
                    CommandErrorsKeys.SALVO_IS_OUT_OF_RANGE_ERROR_MSG
                );

                done();
            }
        });
    });

    describe('buildCommand', () => {
        describe('CrossPoint Connect On Go Group Salvo Acknowledge Message CMD_122_0X7a', () => {
            let commandIdentifier: CommandIdentifier;

            beforeAll(() => {
                commandIdentifier =
                    CommandIdentifiers.TX.GENERAL.CROSSPOINT_CONNECT_ON_GO_GROUP_SALVO_ACKNOWLEDGE_MESSAGE;
            });

            it('Should create & pack the general command (matrixId = 0 - levelId = 0 - destinationId = 0 - sourceId = 0 - salvoId = 0)', () => {
                // Arrange
                const params: CrossPointConnectOnGoGroupSalvoAcknowledgeMessageCommandParams = {
                    matrixId: 0,
                    levelId: 0,
                    destinationId: 0,
                    sourceId: 0,
                    salvoId: 0
                };

                // Act
                const command = new CrossPointConnectOnGoGroupSalvoAcknowledgeMessageCommand(params);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '7a 00 00 00 00 00', // data
                    6, // bytesCount
                    0x80, // checksum
                    '10 02 7a 00 00 00 00 00 06 80 10 03' // buffer
                );
            });
            it('Should create & pack the general command (matrixId = 0 - levelId = 0 - destinationId = 895 - sourceId = 0 - salvoId = doesnt matter)', () => {
                // Arrange
                const params: CrossPointConnectOnGoGroupSalvoAcknowledgeMessageCommandParams = {
                    matrixId: 0,
                    levelId: 0,
                    destinationId: 895,
                    sourceId: 0,
                    salvoId: 0
                };

                // Act
                const command = new CrossPointConnectOnGoGroupSalvoAcknowledgeMessageCommand(params);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '7a 00 60 7f 00 00', // data
                    6, // bytesCount
                    0xa1, // checksum
                    '10 02 7a 00 60 7f 00 00 06 a1 10 03' // buffer
                );
            });
            it('Should create & pack the general command (matrixId = 15 - levelId = 15 - destinationId = 895 - sourceId = 1023 - salvoId = doesnt matter)', () => {
                // Arrange
                const params: CrossPointConnectOnGoGroupSalvoAcknowledgeMessageCommandParams = {
                    matrixId: 15, // 4 bits coded
                    levelId: 15, // 4 bits codeded
                    destinationId: 895, // Multiplier 3 bits coded (896 DIV 128 = 7)
                    sourceId: 1023,
                    salvoId: 0
                };

                // Act
                const command = new CrossPointConnectOnGoGroupSalvoAcknowledgeMessageCommand(params);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '7a ff 67 7f 7f 00', // data
                    6, // bytesCount
                    0x1c, // checksum
                    '10 02 7a ff 67 7f 7f 00 06 1c 10 03' // buffer
                );
            });
        });

        describe('Extended CrossPoint Connect On Go Group Salvo Acknowledge Message CMD_250_0Xfa', () => {
            let commandIdentifier: CommandIdentifier;

            beforeAll(() => {
                commandIdentifier =
                    CommandIdentifiers.TX.EXTENDED.CROSSPOINT_CONNECT_ON_GO_GROUP_SALVO_ACKNOWLEDGE_MESSAGE;
            });
            it('Should create & pack the extended command (matrixId = 16 - levelId = 15 - destinationId = 895 - sourceId = 1023) matrixId > 15 & matrixId = [DLE]', () => {
                // Arrange
                const params: CrossPointConnectOnGoGroupSalvoAcknowledgeMessageCommandParams = {
                    matrixId: 16,
                    levelId: 15,
                    destinationId: 895,
                    sourceId: 1023,
                    salvoId: 0
                };

                // Act
                const command = new CrossPointConnectOnGoGroupSalvoAcknowledgeMessageCommand(params);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    'fa 10 0f 03 7f 03 ff 00', // data
                    8, // bytesCount
                    0x5b, // checksum
                    '10 02 fa 10 10 0f 03 7f 03 ff 00 08 5b 10 03' // buffer
                );
            });

            it('Should create & pack the extended command (matrixId = 15 - levelId = 16 - destinationId = 895 - sourceId = 1023) levelId > 15 & levelId = [DLE]', () => {
                // Arrange
                const params: CrossPointConnectOnGoGroupSalvoAcknowledgeMessageCommandParams = {
                    matrixId: 15,
                    levelId: 16,
                    destinationId: 895,
                    sourceId: 1023,
                    salvoId: 0
                };

                // Act
                const command = new CrossPointConnectOnGoGroupSalvoAcknowledgeMessageCommand(params);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    'fa 0f 10 03 7f 03 ff 00 ', // data
                    8, // bytesCount
                    0x5b, // checksum
                    '10 02 fa 0f 10 10 03 7f 03 ff 00 08 5b 10 03' // buffer
                );
            });

            it('Should create & pack the extended command (matrixId = 0 - levelId = 0 - destinationId = 896 - sourceId = 1023) destinationId > 895', () => {
                // Arrange
                const params: CrossPointConnectOnGoGroupSalvoAcknowledgeMessageCommandParams = {
                    matrixId: 0,
                    levelId: 0,
                    destinationId: 896,
                    sourceId: 1023,
                    salvoId: 0
                };

                // Act
                const command = new CrossPointConnectOnGoGroupSalvoAcknowledgeMessageCommand(params);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    'fa 00 00 03 80 03 ff 00', // data
                    8, // bytesCount
                    0x79, // checksum
                    '10 02 fa 00 00 03 80 03 ff 00 08 79 10 03' // buffer
                );
            });

            it('Should create & pack the extended command (matrixId = 0 - levelId = 0 - destinationId = 0 - sourceId = 1024) sourceId > 1023', () => {
                // Arrange
                const params: CrossPointConnectOnGoGroupSalvoAcknowledgeMessageCommandParams = {
                    matrixId: 0,
                    levelId: 0,
                    destinationId: 0,
                    sourceId: 1024,
                    salvoId: 0
                };

                // Act
                const command = new CrossPointConnectOnGoGroupSalvoAcknowledgeMessageCommand(params);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    'fa 00 00 00 00 04 00 00', // data
                    8, // bytesCount
                    0xfa, // checksum
                    '10 02 fa 00 00 00 00 04 00 00 08 fa 10 03' // buffer
                );
            });

            it('Should create & pack the extended command (matrixId = 255 - levelId = 255 - destinationId = 65534 - sourceId = 65534) [MAX Values] ', () => {
                // Arrange
                const params: CrossPointConnectOnGoGroupSalvoAcknowledgeMessageCommandParams = {
                    matrixId: 255,
                    levelId: 255,
                    destinationId: 65534,
                    sourceId: 65534,
                    salvoId: 0
                };

                // Act
                const command = new CrossPointConnectOnGoGroupSalvoAcknowledgeMessageCommand(params);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    'fa ff ff ff fe ff fe 00', // data
                    8, // bytesCount
                    0x06, // checksum
                    '10 02 fa ff ff ff fe ff fe 00 08 06 10 03' // buffer
                );
            });
        });

        describe('if [DLE] = [0x10] is found it is replaced with [DLE][DLE] to prevent the DATA, BTC or CHK being interpreted as a command', () => {
            // When a DLE is found it is replaced with DLE DLE to prevent the DATA, BTC or CHK being interpreted as a command
            let commandIdentifier: CommandIdentifier;

            beforeAll(() => {
                commandIdentifier =
                    CommandIdentifiers.TX.EXTENDED.CROSSPOINT_CONNECT_ON_GO_GROUP_SALVO_ACKNOWLEDGE_MESSAGE;
            });
            it('Should verify if [DLE] is duplicated (matrixId=16 - levelId=16 - destinationId=16))', () => {
                // Arrange
                const params: CrossPointConnectOnGoGroupSalvoAcknowledgeMessageCommandParams = {
                    matrixId: 16,
                    levelId: 16,
                    destinationId: 16,
                    sourceId: 16,
                    salvoId: 0
                };

                // Act
                const command = new CrossPointConnectOnGoGroupSalvoAcknowledgeMessageCommand(params);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    'fa 10 10 00 10 00 10 00', // data
                    8, // bytesCount
                    0xbe, // checksum
                    '10 02 fa 10 10 10 10 00 10 10 00 10 10 00 08 be 10 03' // buffer
                );
            });
        });
    });
    describe('to Log Description of the general and extended command', () => {
        it('Should log General command description', () => {
            // Arrange
            const params: CrossPointConnectOnGoGroupSalvoAcknowledgeMessageCommandParams = {
                matrixId: 0,
                levelId: 0,
                destinationId: 895,
                sourceId: 0,
                salvoId: 0
            };

            // Act
            const command = new CrossPointConnectOnGoGroupSalvoAcknowledgeMessageCommand(params);
            const description = command.toLogDescription();

            // Assert
            expect(description.toLowerCase().startsWith('general')).toBe(true);
        });
        it('Should log Extended general command description', () => {
            // Arrange
            const params: CrossPointConnectOnGoGroupSalvoAcknowledgeMessageCommandParams = {
                matrixId: 0,
                levelId: 0,
                destinationId: 0,
                sourceId: 1024,
                salvoId: 0
            };

            // Act
            const command = new CrossPointConnectOnGoGroupSalvoAcknowledgeMessageCommand(params);
            const description = command.toLogDescription();

            // Assert
            expect(description.toLowerCase().startsWith('extended')).toBe(true);
        });
    });
});
